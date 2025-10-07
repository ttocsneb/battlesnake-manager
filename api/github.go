package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"

	"github.com/gorilla/mux"
	"github.com/ttocsneb/battlesnake-manager/docker"
)

type buildConfigT struct {
	Secret        []byte
	BuildingMutex sync.Mutex
	Queued        bool
}

var buildConfig map[string]*buildConfigT = map[string]*buildConfigT{}

func RegisterSecret(repoName string, secret string) {
	buildConfig[repoName] = &buildConfigT{
		Secret:        []byte(secret),
		BuildingMutex: sync.Mutex{},
		Queued:        false,
	}

}

func registerGithubHandlers(r *mux.Router) {
	r.HandleFunc("/deploy/", githubWebhookHandler)
}

func checkSecret(payload []byte, secret []byte, sig string) error {
	splits := strings.SplitN(sig, "=", 2)
	if len(splits) != 2 {
		return errors.New("Bad Signature")
	}
	sigType := splits[0]
	digest := splits[1]

	var hsh func() hash.Hash
	if sigType == "sha1" {
		hsh = sha1.New
	} else if sigType == "sha256" {
		hsh = sha256.New
	} else {
		return errors.New("Unsupported hash algorithm")
	}

	hasher := hmac.New(hsh, secret)
	hasher.Write(payload)
	sum, err := hex.DecodeString(digest)
	if err != nil {
		return err
	}

	if !hmac.Equal(hasher.Sum(nil), sum) {
		return errors.New("Signature invalid")
	}
	return nil
}

type repository struct {
	Private       bool   `json:"private"`
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
}

type pushRequest struct {
	Ref        string     `json:"ref"`
	Repository repository `json:"repository"`
}

func githubWebhookHandler(w http.ResponseWriter, r *http.Request) {
	event := r.Header.Get("X-GitHub-Event")
	sig := r.Header.Get("X-Hub-Signature")
	sig256 := r.Header.Get("X-Hub-Signature-256")
	contentType := r.Header.Get("Content-Type")

	agent := r.Header.Get("User-Agent")

	if !strings.HasPrefix(agent, "GitHub-Hookshot/") {
		w.WriteHeader(401)
		w.Write([]byte("Access Denied"))
		return
	}

	if contentType != "application/json" {
		w.WriteHeader(400)
		w.Write([]byte("Invalid Content-Type. Only json Supported"))
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(400)
		w.Write([]byte("Invalid Request"))
		return
	}

	var request pushRequest
	err = json.Unmarshal(body, &request)
	if err != nil {
		w.WriteHeader(400)
		w.Write([]byte("Invalid Request"))
		return
	}

	repoName := request.Repository.FullName

	if sig256 != "" {
		sig = sig256
	}
	conf, found := buildConfig[repoName]
	if !found {
		w.WriteHeader(401)
		w.Write([]byte("Access Denied: "))
		w.Write([]byte("Not registered"))
		return
	}

	if err = checkSecret(body, conf.Secret, sig); err != nil {
		w.WriteHeader(401)
		w.Write([]byte("Access Denied: "))
		w.Write([]byte(err.Error()))
		return
	}

	if event == "ping" {
		w.WriteHeader(200)
		w.Write([]byte("pong"))
		return
	}

	if event != "push" {
		w.WriteHeader(403)
		w.Write([]byte("Action Forbidden"))
		return
	}

	if request.Repository.Private {
		w.WriteHeader(500)
		w.Write([]byte("Cannot Access Private Repos"))
		return
	}

	ref := strings.Split(request.Ref, "/")
	if len(ref) != 3 || ref[0] != "refs" {
		w.WriteHeader(200)
		w.Write([]byte("Ignoring push to "))
		w.Write([]byte(request.Ref))
		w.Write([]byte("\nOnly deploying from branch "))
		w.Write([]byte(request.Repository.DefaultBranch))
		return
	}
	if ref[1] == "tags" {
		w.WriteHeader(200)
		w.Write([]byte("Ignoring push to tag "))
		w.Write([]byte(ref[2]))
		w.Write([]byte("\nOnly deploying from branch "))
		w.Write([]byte(request.Repository.DefaultBranch))
		return
	}
	if ref[1] != "heads" {
		w.WriteHeader(200)
		w.Write([]byte("Ignoring push to "))
		w.Write([]byte(request.Ref))
		w.Write([]byte("\nOnly deploying from branch "))
		w.Write([]byte(request.Repository.DefaultBranch))
		return
	}
	if ref[2] != request.Repository.DefaultBranch {
		w.WriteHeader(200)
		w.Write([]byte("Ignoring push to branch "))
		w.Write([]byte(ref[2]))
		w.Write([]byte("\nOnly deploying from branch "))
		w.Write([]byte(request.Repository.DefaultBranch))
		return
	}

	locked := conf.BuildingMutex.TryLock()
	if !locked {
		w.WriteHeader(200)
		w.Write([]byte("There is already a job deploying\n"))
		w.Write([]byte("Adding the build job to the queue"))
		conf.Queued = true
		return
	}

	go deployApplication(request.Repository.FullName)

	w.WriteHeader(200)
	w.Write([]byte("Deploying "))
	w.Write([]byte(request.Repository.FullName))
	w.Write([]byte("..."))
}

func DeployApplicationPublic(repoName string) {
	conf := buildConfig[repoName]
	if conf == nil {
		fmt.Println("Could not deploy container, not registered")
		return
	}
	conf.BuildingMutex.Lock()

	deployApplication(repoName)
}

func deployApplication(repoName string) {
	containerName := docker.RepoNameToContainerName(repoName)
	fmt.Printf("Deploying container %v...\n", containerName)

	conf := buildConfig[repoName]
	if conf == nil {
		fmt.Println("Could not deploy container, not registered")
		return
	}
	defer func() {
		// When this function finishes, If there was another job queued, re-run
		// the job, otherwise free the mutex
		if conf.Queued {
			conf.Queued = false
			deployApplication(repoName)
		} else {
			conf.BuildingMutex.Unlock()
		}
	}()

	errorLogger := func(msg string, err error) {
		fmt.Printf("While deploying %v\n", repoName)
		fmt.Printf("\t%v:\n", msg)
		fmt.Printf("\t%v\n", err)
	}
	runCmd := func(message string, name string, args ...string) bool {
		cmd := exec.Command(name, args...)
		cmd.Stderr = os.Stderr
		cmd.Stdout = os.Stdout
		err := cmd.Start()
		if err != nil {
			errorLogger(message, err)
			return false
		}
		err = cmd.Wait()
		if err != nil {
			errorLogger(message, err)
			return false
		}
		return true
	}

	exists := true
	_, err := docker.CheckContainer(containerName)
	if err != nil {
		if err == docker.ErrorDoesNotExist {
			exists = false
		} else {
			errorLogger("Could not get container state", err)
			return
		}
	}

	cmd := exec.Command("docker", "images", "--format", "{{ json . }}", repoName)
	cmd.Stderr = os.Stderr
	reader, err := cmd.StdoutPipe()
	if err != nil {
		errorLogger("Could not create stdout pipe", err)
		return
	}
	err = cmd.Start()
	if err != nil {
		errorLogger("Could not get list of container images", err)
		return
	}
	imagesRaw, err := io.ReadAll(reader)
	if err != nil {
		errorLogger("Could not get list of container images", err)
		return
	}
	imageErr := cmd.Wait()
	if imageErr != nil {
		errorLogger("Could not get list of container images", err)
	}

	repoDir, err := os.MkdirTemp("", containerName+"-*")
	if err != nil {
		errorLogger("Could not create tempdir", err)
		return
	}
	defer func() {
		if err := os.RemoveAll(repoDir); err != nil {
			errorLogger(fmt.Sprintf("Could not cleanup %v", repoDir), err)
		}
	}()

	if !runCmd("Could not clone repo", "git", "clone", "https://github.com/"+repoName+".git", repoDir) {
		return
	}
	tag := repoName + ":local"
	if !runCmd("Could not build image", "docker", "build", "-t", repoName, repoDir) {
		return
	}

	if exists {
		err = docker.StopContainer(containerName)
		if err != nil {
			errorLogger("Could not stop container", err)
			return
		}

		if !runCmd("Could not delete container", "docker", "rm", containerName) {
			return
		}
	}
	if !runCmd("Could not create container", "docker", "run", "-d", "--name", containerName, tag) {
		return
	}

	// Update the state machine
	_, err = docker.CheckContainer(containerName)
	if err != nil {
		if err == docker.ErrorDoesNotExist {
			errorLogger("The container was not created", err)
			return
		}
		errorLogger("Could not get the container state", err)
		return
	}

	fmt.Printf("Successfully deployed %v\n", containerName)
	fmt.Println("Cleaning up old images...")

	type imageInfo struct {
		Id string `json:"ID"`
	}
	seenIds := []string{}
	for _, image := range bytes.Split(imagesRaw, []byte("\n")) {
		var img imageInfo
		err := json.Unmarshal(image, &img)
		if err != nil {
			errorLogger("Could not parse image output", err)
			continue
		}

		// Just in case there are multiple tags
		if slices.Contains(seenIds, img.Id) {
			continue
		}
		seenIds = append(seenIds, img.Id)

		if !runCmd("Could not remove old image "+img.Id, "docker", "rmi", img.Id) {
			continue
		}
	}

	fmt.Println("Finished Cleaning up old images")
}
