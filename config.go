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
	Type string
	Url  string

	Modifiers []struct {
		Mod  string
		Type string
		Url  string
	}
}

type ConfigTarget struct {
	Sources      []string
	Dependencies []string

	Configure []string
	Build     []string
	Install   []string
}

type Config struct {
	Project ConfigProject
	Source  map[string]ConfigSource
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

func (cfg *Config) FindSource(tag string) *ConfigSource {
	for sourceTag, source := range cfg.Source {
		if sourceTag != tag {
			continue
		}
		return &source
	}
	return nil
}
