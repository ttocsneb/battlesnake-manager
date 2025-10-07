package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

var ErrorNotRegistered = errors.New("Container not registered")
var ErrorDoesNotExist = errors.New("Container does not exist")

func RepoNameToContainerName(repoName string) string {
	return "bs-" + strings.ReplaceAll(repoName, "/", "-")
}

var client *http.Client = nil
var clientMutex sync.Mutex

const socketPath string = "/var/run/docker.sock"

func dockerExec(req *http.Request) (*http.Response, error) {
	clientMutex.Lock()
	defer clientMutex.Unlock()
	if client == nil {
		tr := &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		}
		client = &http.Client{
			Transport: tr,
		}
	}

	return client.Do(req)
}

func dockerExecJson(req *http.Request, result any) error {
	resp, err := dockerExec(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return ErrorDoesNotExist
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("Returned Status Code %v", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	return nil
}

func dockerExecCmd(container string, action string) error {
	req, _ := http.NewRequest("POST", "http://localhost/containers/"+container+"/"+action, nil)
	resp, err := dockerExec(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 204 || resp.StatusCode == 304 {
		return nil
	}
	if resp.StatusCode == 404 {
		return ErrorDoesNotExist
	}
	return fmt.Errorf("Returned Status Code %v", resp.StatusCode)
}

func WaitForDockerSocket() {
	start := time.Now()
	lastLog := start
	for {
		// Check if the socket exists
		if _, err := os.Stat(socketPath); err == nil {
			// Try connecting to it
			conn, err := net.DialTimeout("unix", socketPath, time.Second)
			if err == nil {
				conn.Close()
				return
			}
		}

		if time.Since(lastLog) > 5 * time.Second {
			 fmt.Printf("docker socket not available after %d seconds\n", int(time.Since(start).Seconds()))
		}

		time.Sleep(500 * time.Millisecond)
	}
}

type ContainerStateJson struct {
	State struct {
		Running bool `json:"Running"`
		Paused  bool `json:"Paused"`
	} `json:"State"`
	NetworkSettings struct {
		IPAddress string `json:"IPAdress"`
		Networks  map[string]struct {
			IPAdress string `json:"IPAddress"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
}

func CheckContainer(name string) (bool, error) {
	if !IsRegistered(name) {
		return false, ErrorNotRegistered
	}
	req, _ := http.NewRequest("GET", "http://localhost/containers/"+name+"/json", nil)
	var result ContainerStateJson
	err := dockerExecJson(req, &result)
	if err != nil {
		return false, err
	}

	ip := result.NetworkSettings.IPAddress
	if ip == "" {
		for _, v := range result.NetworkSettings.Networks {
			if v.IPAdress != "" {
				ip = v.IPAdress
				break
			}
		}
	}

	err = updateState(name, result.State.Running, result.State.Paused, ip)
	if err != nil {
		return false, err
	}

	return result.State.Running && !result.State.Paused, nil
}

func EnsureContainerRunning(name string) error {
	if IsStale(name) {
		_, err := CheckContainer(name)
		if err != nil {
			return err
		}
	}

	state, err := GetState(name)
	if err != nil {
		return err
	}

	if state.Running && state.Paused {
		return UnpauseContainer(name)
	}
	if !state.Running {
		return StartContainer(name)
	}
	return nil
}

func StartContainer(name string) error {
	if !IsRegistered(name) {
		return ErrorNotRegistered
	}
	fmt.Printf("Starting %v\n", name)
	err := dockerExecCmd(name, "start")
	if err != nil {
		return err
	}

	// Force update the ip
	_, err = CheckContainer(name)
	return err
}

func StopContainer(name string) error {
	if !IsRegistered(name) {
		return ErrorNotRegistered
	}
	fmt.Printf("Stopping %v\n", name)
	err := dockerExecCmd(name, "stop")
	if err != nil {
		return err
	}

	return updateRunning(name, false, false)
}
func PauseContainer(name string) error {
	if !IsRegistered(name) {
		return ErrorNotRegistered
	}
	fmt.Printf("Pausing %v\n", name)
	err := dockerExecCmd(name, "pause")
	if err != nil {
		return err
	}

	return updatePaused(name, true)
}
func UnpauseContainer(name string) error {
	if !IsRegistered(name) {
		return ErrorNotRegistered
	}
	fmt.Printf("Unpausing %v\n", name)
	err := dockerExecCmd(name, "unpause")
	if err != nil {
		return err
	}

	return updatePaused(name, false)
}
