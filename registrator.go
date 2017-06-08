/*
 *  Copyright (C) 2016 VSCT
 *
 *  Licensed under the Apache License, Version 2.0 (the "License");
 *  you may not use this file except in compliance with the License.
 *  You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 *  Unless required by applicable law or agreed to in writing, software
 *  distributed under the License is distributed on an "AS IS" BASIS,
 *  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *  See the License for the specific language governing permissions and
 *  limitations under the License.
 *
 */
package main

import (
	"flag"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/docker/engine-api/client"
	"github.com/docker/engine-api/types"
	"github.com/docker/engine-api/types/container"
	eventtypes "github.com/docker/engine-api/types/events"
	"github.com/docker/engine-api/types/filters"
	events "github.com/vdemeester/docker-events"
	registrator "github.com/voyages-sncf-technologies/strowgr-registrator/internal"
	"golang.org/x/net/context"
	"os"
	"strings"
)

var (
	version                                                        bool
	adminUrl                                                       string
	address                                                        string
	debug                                                          bool
	Version, GitCommit, GitBranch, GitState, GitSummary, BuildDate string
)

const (
	APPLICATION_LABEL  = "application.name"
	PLATFORM_LABEL     = "platform.name"
	SERVICE_NAME_LABEL = "service.%s.name"
	ID_NAMING_STRATEGY = "registrator.id_generator"
)

type NamingStrategy func(info types.ContainerJSON, instance *registrator.RegisterCommand) string

func init() {
	log.SetFormatter(new(log.TextFormatter))
}

func main() {
	fmt.Printf("Version: %s\nBuild date: %s\nGitCommit: %s\nGitBranch: %s\nGitState: %s\nGitSummary: %s\n", Version, BuildDate, GitCommit, GitBranch, GitState, GitSummary)

	flag.BoolVar(&debug, "verbose", false, "debug mode")
	flag.BoolVar(&version, "version", false, "Show version")
	flag.StringVar(&adminUrl, "url", "", "Admin url")
	flag.StringVar(&address, "address", "", "Ip address")
	flag.Parse()

	if version {
		os.Exit(0)
	}

	if debug {
		log.SetLevel(log.DebugLevel)
	}

	cli, err := client.NewEnvClient()
	cli.Info(context.Background())

	if err != nil {
		log.WithError(err).Fatal("Unable to start client")
	}

	// Setup the event handler
	eventHandler := events.NewHandler(events.ByAction)
	eventHandler.Handle("start", func(m eventtypes.Message) {

		info, err := cli.ContainerInspect(context.Background(), m.ID)

		var namingStrategy NamingStrategy = getNamingStrategy(info.Config)

		log.WithField("info", info).Debug("Inspect container")
		if err != nil {
			log.WithError(err).WithField("containerId", m.ID).Error("Cannot register instance")
		} else {
			log.WithField("info", info).Debug("Inspect container")
			if info.Config == nil || info.Config.ExposedPorts == nil {
				log.WithField("container", info.Name).Debug("No exposed ports")
			} else {
				if getMetadata(info.Config, APPLICATION_LABEL) == "" {
					log.WithField("container", info.Name).WithField("key", APPLICATION_LABEL).Debug("Metadata is missing")
					return
				}

				if getMetadata(info.Config, PLATFORM_LABEL) == "" {
					log.WithField("container", info.Name).WithField("key", PLATFORM_LABEL).Debug("Metadata is missing")
					return
				}

				isNetHost := info.NetworkSettings.Networks != nil &&  info.NetworkSettings.Networks["host"] != nil

				for exposedPort, _ := range info.Config.ExposedPorts {
					private_port := exposedPort.Port() + "_" + exposedPort.Proto()
					public_ports := info.NetworkSettings.Ports[exposedPort]

					if ! isNetHost && ( public_ports == nil || len(public_ports) == 0 ){
						log.WithField("private_port", private_port).Debug("Port not published")
						continue
					}

					serviceLabel := fmt.Sprintf(SERVICE_NAME_LABEL, private_port)
					if getMetadata(info.Config, serviceLabel) == "" {
						log.WithField("container", info.Name).WithField("label", serviceLabel).Debug("Label is missing")
						continue
					}

					var public_port string
					if isNetHost {
						public_port = exposedPort.Port()
					}else{
						public_port = public_ports[0].HostPort
					}
					log.WithField("port", private_port).Debug("Analyze container")

					instance := registrator.NewInstance()
					instance.Header.Application = getMetadata(info.Config, APPLICATION_LABEL)
					instance.Header.Platform = getMetadata(info.Config, PLATFORM_LABEL)
					instance.Server.BackendId = getMetadata(info.Config, serviceLabel)
					instance.Server.Port = public_port
					instance.Server.Ip = address
					instance.Server.Id = namingStrategy(info, instance)
					instance.Register(adminUrl)
				}
			}
		}
	})

	stoppedOrDead := func(m eventtypes.Message) {
		log.WithField("type", "remove").Info(m.From)
	}
	eventHandler.Handle("die", stoppedOrDead)
	eventHandler.Handle("stop", stoppedOrDead)

	// Filter the events we wams so receive
	filters := filters.NewArgs()
	filters.Add("type", "container")
	options := types.EventsOptions{
		Filters: filters,
	}

	log.Info("Starting")
	errChan := events.MonitorWithHandler(context.Background(), cli, options, eventHandler)

	if err := <-errChan; err != nil {
		log.WithError(err).Error("Error")
	}
}

func getMetadata(config *container.Config, key string) string {
	if config.Labels[key] != "" {
		return config.Labels[key]
	} else {
		return getEnv(config.Env, key)
	}
}

func getEnv(haystack []string, needle string) string {
	for index := range haystack {
		res := strings.Split(haystack[index], "=")
		if res[0] == needle {
			return res[1]
		}
	}
	return ""
}

func getNamingStrategy(config *container.Config) NamingStrategy {
	switch getMetadata(config, ID_NAMING_STRATEGY) {
	case "container_name":
		return containerNamingStrategy
	default:
		return defaultNamingStrategy
	}
}

func defaultNamingStrategy(info types.ContainerJSON, instance *registrator.RegisterCommand) string {
	return strings.Replace(instance.Server.Ip, ".", "_", -1) + "_" + strings.Replace(info.Name, "/", "_", -1) + "_" + instance.Server.Port
}

func containerNamingStrategy(info types.ContainerJSON, instance *registrator.RegisterCommand) string {
	return strings.Replace(info.Name, "/", "", -1) + "_" + instance.Server.BackendId
}
