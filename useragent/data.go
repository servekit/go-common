package useragent

import (
	"embed"
)

// The curated rule data lives in YAML files that mirror the layout of the
// Matomo device-detector regexes (see NOTICE.md), so refreshing a category
// from upstream stays a copy-adapt-trim exercise.
//
//go:embed data/oss.yml data/clients.yml data/devices.yml data/bots.yml
var dataFS embed.FS

// options collects the rule overrides handed to New. A nil category means
// "use the embedded default", and only nil categories read the embedded
// files, so a fully overridden Parser never pays for YAML decoding.
type options struct {
	os      []OSRule
	clients []ClientRule
	devices []DeviceRule
	bots    []BotRule
}

// loadOS compiles the OS rules for New, falling back to embedded data.
func (o options) loadOS() ([]rule, error) {
	defs := o.os
	if defs == nil {
		data, err := dataFS.ReadFile("data/oss.yml")
		if err != nil {
			return nil, err
		}
		if defs, err = loadOSRules(data); err != nil {
			return nil, err
		}
	}
	return compileOSRules(defs)
}

// loadClients compiles the client rules for New, falling back to embedded data.
func (o options) loadClients() ([]rule, error) {
	defs := o.clients
	if defs == nil {
		data, err := dataFS.ReadFile("data/clients.yml")
		if err != nil {
			return nil, err
		}
		if defs, err = loadClientRules(data); err != nil {
			return nil, err
		}
	}
	return compileClientRules(defs)
}

// loadDevices compiles the device rules for New, falling back to embedded data.
func (o options) loadDevices() ([]rule, error) {
	defs := o.devices
	if defs == nil {
		data, err := dataFS.ReadFile("data/devices.yml")
		if err != nil {
			return nil, err
		}
		if defs, err = loadDeviceRules(data); err != nil {
			return nil, err
		}
	}
	return compileDeviceRules(defs)
}

// loadBots compiles the crawler rules for New, falling back to embedded data.
func (o options) loadBots() ([]rule, error) {
	defs := o.bots
	if defs == nil {
		data, err := dataFS.ReadFile("data/bots.yml")
		if err != nil {
			return nil, err
		}
		if defs, err = loadBotRules(data); err != nil {
			return nil, err
		}
	}
	return compileBotRules(defs)
}
