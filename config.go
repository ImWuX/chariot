package main

import (
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

type Config struct {
	Project ConfigProject
	Source  map[string]ConfigSource
	Host    map[string]ConfigTarget
	Target  map[string]ConfigTarget
}

func ReadConfig(path string) Config {
	var conf Config

	data, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}

	if _, err = toml.Decode(string(data), &conf); err != nil {
		panic(err)
	}

	return conf
}

func (cfg *Config) FindTarget(tag string) *ConfigTarget {
	for targetTag, target := range cfg.Target {
		if targetTag != tag {
			continue
		}
		return &target
	}
	return nil
}

func (cfg *Config) FindHostTarget(tag string) *ConfigTarget {
	for hostTargetTag, hostTarget := range cfg.Host {
		if hostTargetTag != tag {
			continue
		}
		return &hostTarget
	}
	return nil
}

func (cfg *Config) FindSource(tag string) *ConfigSource {
	for sourceTag, source := range cfg.Source {
		if sourceTag != tag {
			continue
		}
		return &source
	}
	return nil
}
