package main

import (
	"encoding/json"
	"io/ioutil"
	"strings"
)

type Config struct {
	Paths []Path
}

func readConfig(name string) (*Config, error) {
	if name == "" {
		name = "config.json"
	}
	data, err := ioutil.ReadFile(name)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) FindPath(name string) (Path, bool) {
	for _, p := range c.Paths {
		if strings.HasPrefix(name, p.Name) {
			return p, true
		}
	}
	return Path{}, false
}