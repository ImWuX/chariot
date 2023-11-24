package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

type ChariotOptions struct {
	cache          string
	refetchSources bool
	resetContainer bool
	verbose        bool
	threads        uint
}

type ChariotContext struct {
	options *ChariotOptions
	targets []*ChariotTarget
}

type ChariotTarget struct {
	tag          Tag
	dependencies []*ChariotTarget

	touched bool
	do      bool
}

func main() {
	fmt.Println("Chariot")

	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	config := flag.String("config", "chariot.toml", "Path to the config file")
	cache := flag.String("cache", filepath.Join(cwd, ".chariot-cache"), "Path to the cache directory")
	refetchSources := flag.Bool("refetch-sources", false, "Refetch all sources")
	resetContainer := flag.Bool("reset-container", false, "Create a new container")
	verbose := flag.Bool("verbose", true, "Turn on verbose logging")
	threads := flag.Uint("threads", 8, "Number of simultaneous threads to use")
	// shell := flag.Bool("shell", false, "Open shell into the container")
	flag.Parse()

	cfg := ReadConfig(*config)
	fmt.Printf("Project: %s\n", cfg.Project.Name)

	targets, err := cfg.BuildTargets()
	if err != nil {
		fmt.Println(err)
		return
	}

	doTargets := make([]*ChariotTarget, 0)
	for _, stag := range flag.Args() {
		tag, err := StringToTag(stag)
		if err != nil {
			fmt.Println(err)
			return
		}
		for _, target := range targets {
			if target.tag != tag {
				continue
			}
			target.do = true
			doTargets = append(doTargets, target)
		}
	}

	ctx := &ChariotContext{
		options: &ChariotOptions{
			cache:          *cache,
			refetchSources: *refetchSources,
			resetContainer: *resetContainer,
			verbose:        *verbose,
			threads:        *threads,
		},
		targets: targets,
	}

	for _, target := range doTargets {
		ctx.do(target)
	}
}

func (ctx *ChariotContext) do(target *ChariotTarget) {
	if target.touched {
		return
	}
	target.touched = true

	for _, dep := range target.dependencies {
		ctx.do(dep)
	}

	if !target.do {
		return
	}
	target.do = false

	fmt.Printf(">> %s\n", target.tag.ToString())
}
