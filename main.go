package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/ttocsneb/battlesnake-manager/api"
	"github.com/ttocsneb/battlesnake-manager/docker"
)

type ContainerSetting struct {
	Name   string `json:"name"`
	Secret string `json:"secret"`
}

func loadConfig() error {
	body, err := os.ReadFile("/data/battlesnakes.json")
	if err != nil {
		// The file could not be read
		fmt.Println("Could not load battlesnakes settings")
		fmt.Printf("\t%v\n", err)
		fmt.Println("\ncontinuing without configuration")
		return nil
	}
	var result []ContainerSetting
	if err = json.NewDecoder(bytes.NewReader(body)).Decode(&result); err != nil {
		fmt.Println("Could not load battlesnakes settings")
		fmt.Printf("\t%v\n", err)
		fmt.Println("\ncontinuing without configuration")
		return nil
	}

	for _, val := range result {
		docker.RegisterContainer(val.Name)
		api.RegisterSecret(val.Name, val.Secret)
	}
	return nil
}

func stopOldContainersJob() time.Duration {
	toStop := []string{}
	toPause := []string{}
	nextRunner := time.Hour

	const PAUSE_DELAY time.Duration = time.Minute
	const STOP_DELAY time.Duration = time.Hour

	docker.IterContainers(func(name string, container docker.ContainerState) bool {
		timeSince := 0 * time.Second
		if container.LastUsed != nil {
			timeSince = container.LastUsed.Sub(time.Now())
		}

		if container.Running {
			if !container.Paused {
				if timeSince < PAUSE_DELAY {
					nextRunner = min(nextRunner, PAUSE_DELAY-timeSince)
				} else {
					toPause = append(toPause, name)
				}
			} else {
				if timeSince < STOP_DELAY {
					nextRunner = min(nextRunner, STOP_DELAY-timeSince)
				} else {
					toStop = append(toStop, name)
				}
			}
		}
		return true
	})

	for _, name := range toStop {
		err := docker.StopContainer(name)
		if err != nil {
			fmt.Printf("While stopping old job %v\n", name)
			fmt.Printf("\t%v\n", err)
			continue
		}
		fmt.Printf("Stopped container %v\n", name)
	}
	for _, name := range toPause {
		err := docker.PauseContainer(name)
		if err != nil {
			fmt.Printf("While pausing old job %v\n", name)
			fmt.Printf("\t%v\n", err)
			continue
		}
		fmt.Printf("Paused container %v\n", name)
	}

	return nextRunner
}

func main() {
	err := loadConfig()
	if err != nil {
		panic(err)
	}

	go func() {
		for {
			delay := stopOldContainersJob()
			time.Sleep(delay)
		}
	}()

	err = api.Serve()
	if err != nil {
		panic(err)
	}
}
