// mautrix-wsproxy - A simple HTTP push -> websocket proxy for Matrix appservices.
// Copyright (C) 2021 Tulir Asokan
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package main

import (
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"

	"gopkg.in/yaml.v2"
)

type Config struct {
	ListenAddress string        `yaml:"listen_address"`
	AppServices   []*AppService `yaml:"appservices"`

	SyncProxy SyncProxyConfig `yaml:"sync_proxy"`

	byASToken map[string]*AppService `yaml:"-"`
	byHSToken map[string]*AppService `yaml:"-"`
}

type SyncProxyConfig struct {
	URL          string `yaml:"url"`
	OwnURL       string `yaml:"wsproxy_url"`
	SharedSecret string `yaml:"shared_secret"`
}

func (spc *SyncProxyConfig) MakeURL(appserviceID string) (string, error) {
	spURL, err := url.Parse(cfg.SyncProxy.URL)
	if err != nil {
		return "", fmt.Errorf("failed to parse sync proxy URL: %w", err)
	}
	spURL.Path = fmt.Sprintf("/_matrix/client/unstable/fi.mau.syncproxy/%s", appserviceID)
	return spURL.String(), nil
}

var cfg Config

var configPath = flag.String("config", "config.yaml", "path to config file")

func loadConfig() {
	flag.Parse()
	if *configPath == "env" {
		cfg.ListenAddress = os.Getenv("LISTEN_ADDRESS")
		cfg.AppServices = []*AppService{{
			ID: os.Getenv("APPSERVICE_ID"),
			AS: os.Getenv("AS_TOKEN"),
			HS: os.Getenv("HS_TOKEN"),
		}}
		cfg.SyncProxy.URL = os.Getenv("SYNC_PROXY_URL")
		cfg.SyncProxy.OwnURL = os.Getenv("SYNC_PROXY_WSPROXY_URL")
		cfg.SyncProxy.SharedSecret = os.Getenv("SYNC_PROXY_SHARED_SECRET")
		if len(cfg.ListenAddress) == 0 {
			log.Fatalln("LISTEN_ADDRESS environment variable is not set")
		} else if len(cfg.AppServices[0].ID) == 0 {
			log.Fatalln("APPSERVICE_ID environment variable is not set")
		} else if len(cfg.AppServices[0].AS) == 0 {
			log.Fatalln("AS_TOKEN environment variable is not set")
		} else if len(cfg.AppServices[0].HS) == 0 {
			log.Fatalln("HS_TOKEN environment variable is not set")
		}
		log.Printf("Found one appservice from environment variables")
	} else {
		file, err := os.Open(*configPath)
		if err != nil {
			log.Fatalln("Failed to open config:", err)
		}
		err = yaml.NewDecoder(file).Decode(&cfg)
		if err != nil {
			log.Fatalln("Failed to read config:", err)
		} else if len(cfg.AppServices) == 0 {
			log.Fatalln("No appservices configured")
		} else if len(cfg.ListenAddress) == 0 {
			log.Fatalln("Listen address not configured")
		}
		appservices := "appservices"
		if len(cfg.AppServices) == 1 {
			appservices = "appservice"
		}
		log.Println("Found", len(cfg.AppServices), appservices, "in", *configPath)
	}
	cfg.byHSToken = make(map[string]*AppService)
	cfg.byASToken = make(map[string]*AppService)
	for i, az := range cfg.AppServices {
		if len(az.ID) == 0 {
			log.Fatalf("Appservice #%d doesn't have an ID", i+1)
		} else if len(az.AS) == 0 {
			log.Fatalf("Appservice %s doesn't have the AS token set", az.ID)
		} else if len(az.AS) == 0 {
			log.Fatalf("Appservice %s doesn't have the HS token set", az.ID)
		}
		cfg.byASToken[az.AS] = az
		cfg.byHSToken[az.HS] = az
	}
}
