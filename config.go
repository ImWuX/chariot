package main

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

type ConfigProject struct {
	Name string
}

type ConfigTarget struct {
	Dependencies []string
}

type ConfigSourceTarget struct {
	ConfigTarget

	Type string
	Url  string

	Modifiers []struct {
		Type   string
		Source string
		File   string
		Cmd    string
	}
}

type ConfigStandardTarget struct {
	ConfigTarget

	Configure []string
	Build     []string
	Install   []string
}

type ConfigHostTarget struct {
	ConfigStandardTarget
	RuntimeDependencies []string `toml:"runtime-dependencies"`
}

type Config struct {
	Project ConfigProject
	Source  map[string]ConfigSourceTarget
	Host    map[string]ConfigHostTarget
	Target  map[string]ConfigStandardTarget
}

func ReadConfig(path string) *Config {
	var cfg Config

	data, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}

	if _, err = toml.Decode(string(data), &cfg); err != nil {
		panic(err)
	}

	return &cfg
}

func (cfg *Config) BuildTargets(ctx *Context) ([]*Target, error) {
	var ensureTarget func(tag Tag) (*Target, error)
	var ensureTargets func(tags []Tag) ([]*Target, error)
	var findTarget func(tag Tag) *Target

	targets := make([]*Target, 0)
	findTarget = func(tag Tag) *Target {
		for _, target := range targets {
			if target.tag != tag {
				continue
			}
			return target
		}
		return nil
	}

	ensureTargets = func(tags []Tag) ([]*Target, error) {
		eTargets := make([]*Target, 0)
		for _, tag := range tags {
			target, err := ensureTarget(tag)
			if err != nil {
				return nil, err
			}
			eTargets = append(eTargets, target)
		}
		return eTargets, nil
	}

	ensureTarget = func(tag Tag) (*Target, error) {
		target := findTarget(tag)
		if target != nil {
			return target, nil
		}

		target = &Target{
			tag:                 tag,
			runtimeDependencies: make([]*Target, 0),
		}
		// any error will stop the build, so we can assume that tag is "valid"
		targets = append(targets, target)

		switch tag.kind {
		case "source":
			cfgSource := cfg.FindSource(tag.id)
			if cfgSource == nil {
				return nil, fmt.Errorf("undefined target (%s)", tag.ToString())
			}

			source := SourceTarget{
				Target:     target,
				sourceType: cfgSource.Type,
				url:        cfgSource.Url,
				modifiers:  make([]SourceModifier, 0),
			}

			deps, err := StringsToTags(cfgSource.Dependencies)
			if err != nil {
				return nil, err
			}
			target.dependencies, err = ensureTargets(deps)
			if err != nil {
				return nil, err
			}

			for _, modifier := range cfgSource.Modifiers {
				var modTarget *Target = nil
				if modifier.Source != "" {
					modTag, err := CreateTag(modifier.Source, "source")
					if err != nil {
						return nil, err
					}
					modTarget, err = ensureTarget(modTag)
					if err != nil {
						return nil, err
					}
				}
				source.modifiers = append(source.modifiers, SourceModifier{
					modifierType: modifier.Type,
					source:       modTarget,
					file:         modifier.File,
					cmd:          modifier.Cmd,
				})
				if modTarget != nil {
					source.dependencies = append(source.dependencies, modTarget)
				}
			}

			target.do = ctx.makeSourceDoer(&source)
		case "host":
			cfgHost := cfg.FindHost(tag.id)
			if cfgHost == nil {
				return nil, fmt.Errorf("undefined target (%s)", tag.ToString())
			}

			host := &HostTarget{
				Target:    target,
				configure: cfgHost.Configure,
				build:     cfgHost.Build,
				install:   cfgHost.Install,
			}

			deps, err := StringsToTags(cfgHost.Dependencies)
			if err != nil {
				return nil, err
			}
			target.dependencies, err = ensureTargets(deps)
			if err != nil {
				return nil, err
			}

			runtimeDeps, err := StringsToTags(cfgHost.RuntimeDependencies)
			if err != nil {
				return nil, err
			}
			for _, runDep := range runtimeDeps {
				runTarget, err := ensureTarget(runDep)
				if err != nil {
					return nil, err
				}
				host.runtimeDependencies = append(host.runtimeDependencies, runTarget)
			}

			target.do = ctx.makeHostDoer(host)
		case "":
			cfgStandard := cfg.FindTarget(tag.id)
			if cfgStandard == nil {
				return nil, fmt.Errorf("undefined target (%s)", tag.ToString())
			}

			std := StandardTarget{
				Target:    target,
				configure: cfgStandard.Configure,
				build:     cfgStandard.Build,
				install:   cfgStandard.Install,
			}

			deps, err := StringsToTags(cfgStandard.Dependencies)
			if err != nil {
				return nil, err
			}
			target.dependencies, err = ensureTargets(deps)
			if err != nil {
				return nil, err
			}

			target.do = ctx.makeStandardDoer(&std)
		}

		return target, nil
	}

	for id := range cfg.Source {
		tag, err := CreateTag(id, "source")
		if err != nil {
			return nil, err
		}
		_, err = ensureTarget(tag)
		if err != nil {
			return nil, err
		}
	}
	for id := range cfg.Host {
		tag, err := CreateTag(id, "host")
		if err != nil {
			return nil, err
		}
		_, err = ensureTarget(tag)
		if err != nil {
			return nil, err
		}
	}
	for id := range cfg.Target {
		tag, err := CreateTag(id, "")
		if err != nil {
			return nil, err
		}
		_, err = ensureTarget(tag)
		if err != nil {
			return nil, err
		}
	}

	return targets, nil
}

func (cfg *Config) FindTarget(id string) *ConfigStandardTarget {
	for targetId, target := range cfg.Target {
		if targetId != id {
			continue
		}
		return &target
	}
	return nil
}

func (cfg *Config) FindHost(id string) *ConfigHostTarget {
	for hostId, host := range cfg.Host {
		if hostId != id {
			continue
		}
		return &host
	}
	return nil
}

func (cfg *Config) FindSource(id string) *ConfigSourceTarget {
	for sourceId, source := range cfg.Source {
		if sourceId != id {
			continue
		}
		return &source
	}
	return nil
}
