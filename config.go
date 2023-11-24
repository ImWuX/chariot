package main

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

type ConfigProject struct {
	Name    string
	Threads int
}

type ConfigSource struct {
	Dependencies []string

	Type string
	Url  string

	Modifiers []struct {
		Type   string
		Source string
		File   string
		Cmd    string
	}
}

type ConfigTarget struct {
	Dependencies []string

	Configure []string
	Build     []string
	Install   []string
}

type ConfigHost struct {
	ConfigTarget
	RuntimeDependencies []string `toml:"runtime-dependencies"`
}

type Config struct {
	Project ConfigProject
	Source  map[string]ConfigSource
	Host    map[string]ConfigHost
	Target  map[string]ConfigTarget
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

func (cfg *Config) BuildTargets() ([]*ChariotTarget, error) {
	targets := make([]*ChariotTarget, 0)
	findTarget := func(tag Tag) *ChariotTarget {
		for _, target := range targets {
			if target.tag != tag {
				continue
			}
			return target
		}
		return nil
	}

	var ensureTarget func(tag Tag) (*ChariotTarget, error)
	ensureTarget = func(tag Tag) (*ChariotTarget, error) {
		target := findTarget(tag)
		if target != nil {
			return target, nil
		}

		var deps []Tag
		var err error
		switch tag.kind {
		case "source":
			source := cfg.FindSource(tag.id)
			if source == nil {
				return nil, fmt.Errorf("undefined target (%s)", tag.ToString())
			}
			deps, err = StringsToTags(source.Dependencies)
			if err != nil {
				return nil, err
			}
			for _, modififer := range source.Modifiers {
				if modififer.Source == "" {
					continue
				}
				modTag, err := CreateTag(modififer.Source, "source")
				if err != nil {
					return nil, err
				}
				deps = append(deps, modTag)
			}
		case "host":
			host := cfg.FindHost(tag.id)
			if host == nil {
				return nil, fmt.Errorf("undefined target (%s)", tag.ToString())
			}
			deps, err = StringsToTags(host.Dependencies)
			if err != nil {
				return nil, err
			}
			runtimeDeps, err := StringsToTags(host.RuntimeDependencies)
			if err != nil {
				return nil, err
			}
			deps = append(deps, runtimeDeps...)
		case "":
			trg := cfg.FindTarget(tag.id)
			if trg == nil {
				return nil, fmt.Errorf("undefined target (%s)", tag.ToString())
			}
			deps, err = StringsToTags(trg.Dependencies)
			if err != nil {
				return nil, err
			}
		}

		target = &ChariotTarget{
			tag:          tag,
			dependencies: make([]*ChariotTarget, 0),
		}
		targets = append(targets, target)

		for _, dep := range deps {
			depTarget, err := ensureTarget(dep)
			if err != nil {
				return nil, err
			}
			target.dependencies = append(target.dependencies, depTarget)
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

func (cfg *Config) FindTarget(id string) *ConfigTarget {
	for targetId, target := range cfg.Target {
		if targetId != id {
			continue
		}
		return &target
	}
	return nil
}

func (cfg *Config) FindHost(id string) *ConfigHost {
	for hostId, host := range cfg.Host {
		if hostId != id {
			continue
		}
		return &host
	}
	return nil
}

func (cfg *Config) FindSource(id string) *ConfigSource {
	for sourceId, source := range cfg.Source {
		if sourceId != id {
			continue
		}
		return &source
	}
	return nil
}
