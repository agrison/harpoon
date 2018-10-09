package main

import (
	"bufio"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	// "strings"
	"time"

	"github.com/NoahShen/gotunnelme/src/gotunnelme" // for tunneling
	"github.com/fatih/color"
	"github.com/gorilla/mux"
)

const (
	headerEvent     = "X-GitHub-Event"  // HTTP header where the webhook event is stored
	headerSignature = "X-Hub-Signature" // HTTP header where the sha1 signature of the payload is stored
)

var (
	config            = tomlConfig{} // the program config
	verbose           = false        // weither we should log the output of the command
	verboseTunnel     = false        // weither we should log the output of the tunneling
	configFile        = ""
	gitHubSecretToken = os.Getenv("GITHUB_HOOK_SECRET_TOKEN") // the webhook secret token, used to verify signature
)

// HookHandler receive hooks from GitHub.
func HookHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	// read the HTTP request body
	payload, err := ioutil.ReadAll(r.Body)
	if err != nil {
		fmt.Fprintln(os.Stderr, color.RedString("Error: "+err.Error()))
		BadRequestHandler(w, r)
		return
	}

	// validate signature
	if gitHubSecretToken != "" {
		sign := r.Header.Get(headerSignature)

		// to compute the HMAC in order to check for equality with what has been sent by GitHub
		mac := hmac.New(sha1.New, []byte(gitHubSecretToken))
		mac.Write(payload)
		expectedHash := hex.EncodeToString(mac.Sum(nil))
		receivedHash := sign[5:] // remove 'sha1='

		// signature mismatch, do not process
		if !hmac.Equal([]byte(receivedHash), []byte(expectedHash)) {
			color.Set(color.FgRed)
			fmt.Fprintf(os.Stderr, "Mismatch between expected (%s) and received (%s) hash.", expectedHash, receivedHash)
			color.Set(color.Faint)
			BadRequestHandler(w, r)
			return
		}
	}

	var eventPayload HookWithRepository
	json.Unmarshal(payload, &eventPayload)

	// verify that this is an event that we should process
	// event := r.Header.Get(headerEvent)
	event := eventPayload.EventName
	if event == "ping" {
		return // always respond 200 to pings
	}

	// check whether we're interested in that event
	if shouldHandleEvent(config.Events, event, eventPayload) {
		handleEvent(event, eventPayload, []byte(payload))
	} else {
		if verbose {
			color.Set(color.FgRed)
			fmt.Fprintf(os.Stderr, "Discarding %s on %s with ref %s.\n",
				color.CyanString(event), color.YellowString(eventPayload.Project.PathWithNamespace), color.YellowString(eventPayload.Ref))
			color.Set(color.Faint)
			BadRequestHandler(w, r)
			return // 400 Bad Request
		}
	}

}

func shouldHandleEvent(events map[string]event, event string, eventPayload HookWithRepository) bool {
	if _, ok := events[event+":"+eventPayload.Project.PathWithNamespace+":"+eventPayload.Ref]; ok {
		return true
	} else if _, ok := events[event+":"+eventPayload.Project.PathWithNamespace+":all"]; ok {
		return true
	} else if _, ok := events[event+":all:all"]; ok {
		return true
	}
	return false
}

// handleEvent handles any event.
func handleEvent(event string, hook HookWithRepository, payload []byte) {

	// prepare the command
	eventKey := event + ":" + hook.Project.PathWithNamespace + ":" + hook.Ref
	if _, ok := config.Events[eventKey]; !ok {
		eventKey = event + ":" + hook.Project.PathWithNamespace + ":all"
	}
	if _, ok := config.Events[eventKey]; !ok {
		eventKey = event + ":all:all"
	}
	cmd := exec.Command(config.Events[eventKey].Cmd,
		event,
		hook.Project.PathWithNamespace,
		hook.Ref,
		// strings.Split(config.Events[eventKey].Args, " ")...,
	)
	// show related commits if push event
	if event == "push" {
		var pushEvent HookPush
		json.Unmarshal(payload, &pushEvent)
		fmt.Println(event, "detected on", color.YellowString(hook.Project.PathWithNamespace),
			"with ref", color.YellowString(hook.Ref), "with the following commits:")
		for _, commit := range pushEvent.Commits {
			fmt.Printf("\t%s - %s by %s\n", commit.Timestamp, color.CyanString(commit.Message), color.BlueString(commit.Author.Name))
		}
		// debug
		b, err := json.MarshalIndent(pushEvent.Commits, "", "  ")
		if err != nil {
			fmt.Println("error:", err)
		}
		fmt.Print(string(b))
		// end debug
		commit := pushEvent.Commits[0]
		cmd = exec.Command(config.Events[eventKey].Cmd,
			event,
			hook.Project.PathWithNamespace,
			hook.Ref,
			commit.Timestamp.String(),
			commit.Message,
			commit.Author.Name,
			// strings.Split(config.Events[eventKey].Args, " ")...,
		)

	}

	// in case of -verbose we log the output of the executed command
	if verbose {
		cmdReader, err := cmd.StdoutPipe()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error creating StdoutPipe for Cmd", err)
			return
		}
		scanner := bufio.NewScanner(cmdReader)
		go func() {
			for scanner.Scan() {
				color.White("> " + scanner.Text() + "\n")
			}
		}()
		cmdReader, err = cmd.StderrPipe()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error creating StderrPipe for Cmd", err)
			return
		}
		scanner = bufio.NewScanner(cmdReader)
		go func() {
			for scanner.Scan() {
				color.Yellow("> " + scanner.Text() + "\n")
			}
		}()
	}

	// launch it
	err := cmd.Start()
	if err != nil {
		color.Set(color.FgRed)
		fmt.Fprintln(os.Stderr, "Error starting Cmd ", cmd.Path, cmd.Args, err)
		color.Set(color.Faint)
		return
	}
}

// BadRequestHandler handles bad requests. Status 400 and JSON error message.
func BadRequestHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusBadRequest)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write([]byte(`{"message": "I don't know what you're talking about"}`))
}

// HeyHandler handles GET request on /.
func HeyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(`Hey, what's up?`))
}

func main() {
	flag.BoolVar(&verbose, "v", false, "Whether we output stuff.")
	flag.BoolVar(&verboseTunnel, "vt", false, "Whether we output stuff regarding tunneling.")
	flag.StringVar(&configFile, "c", "", "config file to load other than ./config.toml")
	flag.Parse()

	// load the config.toml
	config = loadConfig()
	addr := config.Addr + ":" + strconv.Itoa(config.Port)
	color.White(`~~~~__                                     
~~~/ /_  ____ __________  ____  ____  ____ 
~~/ __ \/ __ ` + "`" + `/ ___/ __ \/ __ \/ __ \/ __ \
~/ / / / /_/ / /  / /_/ / /_/ / /_/ / / / /
/_/ /_/\__,_/_/  / .___/\____/\____/_/ /_/ 
~~~~~~~~~~~~~~~~/_/                        
`)
	color.White("\tListening on " + addr)
	readyToListen := false

	if config.Tunnel {
		if verboseTunnel {
			gotunnelme.Debug = true
		}
		tunnel := gotunnelme.NewTunnel()
		url, err := tunnel.GetUrl(config.TunnelName)
		if err != nil {
			panic("Could not get localtunnel.me URL. " + err.Error())
		}
		go func() {
			for !readyToListen {
				time.Sleep(1 * time.Second)
			}
			color.Cyan("\tTunnel URL: " + url)
			err := tunnel.CreateTunnel(config.Port)
			if err != nil {
				panic("Could not create tunnel. " + err.Error())
			}
		}()
	}

	// router & server
	r := mux.NewRouter()
	r.HandleFunc("/", HookHandler).Methods("POST")
	r.HandleFunc("/", HeyHandler).Methods("GET")
	readyToListen = true
	http.ListenAndServe(addr, r)
}
