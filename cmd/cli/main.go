package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	eduvpn "github.com/jwijenbergh/eduvpn-common"
)

// Open a browser with xdg-open
func openBrowser(urlString string) {
	fmt.Printf("OAuth: Initialized with AuthURL %s\n", urlString)
	fmt.Println("OAuth: Opening browser with xdg-open...")
	exec.Command("xdg-open", urlString).Start()
}

// Taken from internal/server.go as it's an internal API for now
// These are used to parse the profile info
type ServerProfile struct {
	ID             string   `json:"profile_id"`
	DisplayName    string   `json:"display_name"`
	VPNProtoList   []string `json:"vpn_proto_list"`
	DefaultGateway bool     `json:"default_gateway"`
}
type ServerProfileInfo struct {
	Current string `json:"current_profile"`
	Info    struct {
		ProfileList []ServerProfile `json:"profile_list"`
	} `json:"info"`
}

// Ask for a profile in the command line
func sendProfile(state *eduvpn.VPNState, data string) {
	fmt.Printf("Multiple VPN profiles found. Please select a profile by entering e.g. 1")
	serverProfiles := &ServerProfileInfo{}

	jsonErr := json.Unmarshal([]byte(data), &serverProfiles)

	if jsonErr != nil {
		fmt.Println("\nFailed to get profile list", jsonErr)
		return
	}

	var profiles string

	for index, profile := range serverProfiles.Info.ProfileList {
		profiles += fmt.Sprintf("\n%d - %s", index+1, profile.DisplayName)
	}

	// Show the profiles
	fmt.Println(profiles)

	var chosenProfile int
	_, scanErr := fmt.Scanf("%d", &chosenProfile)

	if scanErr != nil || chosenProfile <= 0 || chosenProfile > len(serverProfiles.Info.ProfileList) {
		fmt.Println("invalid profile chosen, please retry")
		sendProfile(state, data)
		return
	}

	profile := serverProfiles.Info.ProfileList[chosenProfile-1]
	fmt.Println("Sending profile ID", profile.ID)
	profileErr := state.SetProfileID(profile.ID)

	if profileErr != nil {
		fmt.Println("Failed setting profile with error", profileErr)
	}
}

// The callback function
// If OAuth is started we open the browser with the Auth URL
// If we ask for a profile, we send the profile using command line input
func stateCallback(state *eduvpn.VPNState, oldState string, newState string, data string) {
	if newState == "OAuth_Started" {
		openBrowser(data)
	}

	if newState == "Ask_Profile" {
		sendProfile(state, data)
	}
}

// Get a config for Institute Access or Secure Internet Server
func getConfig(state *eduvpn.VPNState, url string, isInstitute bool) (string, string, error) {
	if !strings.HasPrefix(url, "https://") {
		url = "https://" + url
	}
	if !strings.HasSuffix(url, "/") {
		url += "/"
	}
	// Force TCP is set to False
	if isInstitute {
		return state.GetConfigInstituteAccess(url, false)
	}
	return state.GetConfigSecureInternet(url, false)
}

// A discovery entry for a server
type ServerDiscoEntry struct {
	ServerType string `json:"server_type"`
	BaseURL    string `json:"base_url"`
}

// Gets all different Secure Internet server by parsing the JSON from the discovery
func getAllSecureInternetServers(serverList string) ([]string, error) {
	var secureInternet []string

	discoEntries := []ServerDiscoEntry{}

	jsonErr := json.Unmarshal([]byte(serverList), &discoEntries)

	if jsonErr != nil {
		return nil, jsonErr
	}

	for _, entry := range discoEntries {
		if entry.ServerType == "secure_internet" {
			secureInternet = append(secureInternet, entry.BaseURL)
		}
	}

	return secureInternet, nil
}

// Store the Secure Internet config in a certificate folder
func storeSecureInternetConfig(state *eduvpn.VPNState, url string, directory string) {
	os.MkdirAll(directory, os.ModePerm)

	fmt.Println("Creating and storing cert for", url)

	config, _, configErr := getConfig(state, url, false)

	if configErr != nil {
		fmt.Printf("Failed obtaining config for url %s with error %v\n", url, configErr)
		return
	}

	cleanURL := filepath.Base(url)

	writeErr := os.WriteFile(path.Join(directory, cleanURL), []byte(config), 0o644)
	if writeErr != nil {
		fmt.Printf("Failed writing config for url %s with error %v\n", url, writeErr)
	}
}

// This is basically used to get a certificate for all Secure Internet servers
func getSecureInternetAll(homeURL string) {
	state := &eduvpn.VPNState{}

	state.Register("org.eduvpn.app.linux", "configs", func(old string, new string, data string) {
		stateCallback(state, old, new, data)
	}, true)

	defer state.Deregister()

	// Get the disco servers
	servers, serversErr := state.GetDiscoServers()

	if serversErr != nil {
		fmt.Println("Cannot obtain servers", serversErr)
		return
	}

	secureInternetURLs, secureInternetErr := getAllSecureInternetServers(servers)

	if secureInternetErr != nil {
		fmt.Println("Cannot parse secure internet servers", secureInternetErr)
		return
	}

	// Ensure that the directory exists
	directory := "certs"
	os.MkdirAll(directory, os.ModePerm)

	// Obtain config for home server
	storeSecureInternetConfig(state, homeURL, directory)

	for _, serverURL := range secureInternetURLs {
		if !strings.Contains(serverURL, homeURL) {
			storeSecureInternetConfig(state, serverURL, directory)
		}
	}

	fmt.Println("Done storing all certs in directory:", directory)
}

// Get a config for a single server, Institute Access or Secure Internet
func printConfig(url string, isInstitute bool) {
	state := &eduvpn.VPNState{}

	state.Register("org.eduvpn.app.linux", "configs", func(old string, new string, data string) {
		stateCallback(state, old, new, data)
	}, true)

	defer state.Deregister()

	config, _, configErr := getConfig(state, url, isInstitute)

	if configErr != nil {
		fmt.Println("Error getting config", configErr)
		return
	}

	fmt.Println("Obtained config", config)
}

// The main function
// It parses the arguments and executes the correct functions
func main() {
	urlArg := flag.String("get-institute", "", "The url of an institute to connect to")
	secureInternet := flag.String("get-secure", "", "Gets secure internet servers.")
	secureInternetAll := flag.String("get-secure-all", "", "Gets certificates for all secure internet servers. It stores them in ./certs. Provide an URL for the home server e.g. nl.eduvpn.org.")
	flag.Parse()

	// Connect to a VPN by getting an Institute Access config
	urlString := *urlArg
	secureInternetString := *secureInternet
	secureInternetAllString := *secureInternetAll
	if urlString != "" {
		printConfig(urlString, true)
		return
	} else if secureInternetString != "" {
		printConfig(secureInternetString, false)
		return
	} else if secureInternetAllString != "" {
		getSecureInternetAll(secureInternetAllString)
		return
	}

	flag.PrintDefaults()
}
