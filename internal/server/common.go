package server

import (
	"fmt"
	"time"

	"github.com/eduvpn/eduvpn-common/internal/oauth"
	"github.com/eduvpn/eduvpn-common/internal/util"
	"github.com/eduvpn/eduvpn-common/internal/wireguard"
	"github.com/eduvpn/eduvpn-common/types"
)

// The base type for servers
type ServerBase struct {
	URL            string            `json:"base_url"`
	DisplayName    map[string]string `json:"display_name"`
	SupportContact []string          `json:"support_contact"`
	Endpoints      ServerEndpoints   `json:"endpoints"`
	Profiles       ServerProfileInfo `json:"profiles"`
	ProfilesRaw    string            `json:"profiles_raw"`
	StartTime      time.Time         `json:"start_time"`
	EndTime        time.Time         `json:"expire_time"`
	Type           string            `json:"server_type"`
}

type ServerType int8

const (
	CustomServerType ServerType = iota
	InstituteAccessServerType
	SecureInternetServerType
)

type Servers struct {
	// A custom server is just an institute access server under the hood
	CustomServers            InstituteAccessServers   `json:"custom_servers"`
	InstituteServers         InstituteAccessServers   `json:"institute_servers"`
	SecureInternetHomeServer SecureInternetHomeServer `json:"secure_internet_home"`
	IsType                   ServerType               `json:"is_secure_internet"`
}

type Server interface {
	// Gets the current OAuth object
	GetOAuth() *oauth.OAuth

	// Get the authorization URL template function
	GetTemplateAuth() func(string) string

	// Gets the server base
	GetBase() (*ServerBase, error)
}

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

func (info ServerProfileInfo) GetCurrentProfileIndex() int {
	index := 0
	for _, profile := range info.Info.ProfileList {
		if profile.ID == info.Current {
			return index
		}
		index += 1
	}
	// Default is 'first' profile
	return 0
}

type ServerEndpointList struct {
	API           string `json:"api_endpoint"`
	Authorization string `json:"authorization_endpoint"`
	Token         string `json:"token_endpoint"`
}

// Struct that defines the json format for /.well-known/vpn-user-portal"
type ServerEndpoints struct {
	API struct {
		V2 ServerEndpointList `json:"http://eduvpn.org/api#2"`
		V3 ServerEndpointList `json:"http://eduvpn.org/api#3"`
	} `json:"api"`
	V string `json:"v"`
}

// Make this a var which we can overwrite in the tests
var WellKnownPath string = "/.well-known/vpn-user-portal"

func (servers *Servers) GetCurrentServer() (Server, error) {
	errorMessage := "failed getting current server"
	if servers.IsType == SecureInternetServerType {
		if !servers.HasSecureLocation() {
			return nil, &types.WrappedErrorMessage{
				Message: errorMessage,
				Err:     &ServerGetCurrentNotFoundError{},
			}
		}
		return &servers.SecureInternetHomeServer, nil
	}

	serversStruct := &servers.InstituteServers

	if servers.IsType == CustomServerType {
		serversStruct = &servers.CustomServers
	}
	currentServerURL := serversStruct.CurrentURL
	bases := serversStruct.Map
	if bases == nil {
		return nil, &types.WrappedErrorMessage{
			Message: errorMessage,
			Err:     &ServerGetCurrentNoMapError{},
		}
	}
	server, exists := bases[currentServerURL]

	if !exists || server == nil {
		return nil, &types.WrappedErrorMessage{
			Message: errorMessage,
			Err:     &ServerGetCurrentNotFoundError{},
		}
	}
	return server, nil
}

func (servers *Servers) addInstituteAndCustom(
	discoServer *types.DiscoveryServer,
	isCustom bool,
) (Server, error) {
	url := discoServer.BaseURL
	errorMessage := fmt.Sprintf("failed adding institute access server: %s", url)
	toAddServers := &servers.InstituteServers
	serverType := InstituteAccessServerType

	if isCustom {
		toAddServers = &servers.CustomServers
		serverType = CustomServerType
	}

	if toAddServers.Map == nil {
		toAddServers.Map = make(map[string]*InstituteAccessServer)
	}

	server, exists := toAddServers.Map[url]

	// initialize the server if it doesn't exist yet
	if !exists {
		server = &InstituteAccessServer{}
	}

	instituteInitErr := server.init(
		url,
		discoServer.DisplayName,
		discoServer.Type,
		discoServer.SupportContact,
	)
	if instituteInitErr != nil {
		return nil, &types.WrappedErrorMessage{Message: errorMessage, Err: instituteInitErr}
	}
	toAddServers.Map[url] = server
	servers.IsType = serverType
	return server, nil
}

func (servers *Servers) AddInstituteAccessServer(
	instituteServer *types.DiscoveryServer,
) (Server, error) {
	return servers.addInstituteAndCustom(instituteServer, false)
}

func (servers *Servers) AddCustomServer(
	customServer *types.DiscoveryServer,
) (Server, error) {
	return servers.addInstituteAndCustom(customServer, true)
}

func (servers *Servers) GetSecureLocation() string {
	return servers.SecureInternetHomeServer.CurrentLocation
}

func (servers *Servers) SetSecureLocation(
	chosenLocationServer *types.DiscoveryServer,
) error {
	errorMessage := "failed to set secure location"
	// Make sure to add the current location
	_, addLocationErr := servers.SecureInternetHomeServer.addLocation(chosenLocationServer)

	if addLocationErr != nil {
		return &types.WrappedErrorMessage{Message: errorMessage, Err: addLocationErr}
	}

	servers.SecureInternetHomeServer.CurrentLocation = chosenLocationServer.CountryCode
	return nil
}

func (servers *Servers) AddSecureInternet(
	secureOrg *types.DiscoveryOrganization,
	secureServer *types.DiscoveryServer,
) (Server, error) {
	errorMessage := "failed adding secure internet server"
	// If we have specified an organization ID
	// We also need to get an authorization template
	initErr := servers.SecureInternetHomeServer.init(secureOrg, secureServer)

	if initErr != nil {
		return nil, &types.WrappedErrorMessage{Message: errorMessage, Err: initErr}
	}

	servers.IsType = SecureInternetServerType
	return &servers.SecureInternetHomeServer, nil
}

func ShouldRenewButton(server Server) bool {
	base, baseErr := server.GetBase()

	if baseErr != nil {
		// FIXME: Log error here?
		return false
	}

	// Get current time
	current := util.GetCurrentTime()

	// Session is expired
	if !current.Before(base.EndTime) {
		return true
	}

	// 30 minutes have not passed
	if !current.After(base.StartTime.Add(30 * time.Minute)) {
		return false
	}

	// Session will not expire today
	if !current.Add(24 * time.Hour).After(base.EndTime) {
		return false
	}

	// Session duration is less than 24 hours but not 75% has passed
	duration := base.EndTime.Sub(base.StartTime)
	percentTime := base.StartTime.Add((duration / 4) * 3)
	if duration < time.Duration(24*time.Hour) && !current.After(percentTime) {
		return false
	}

	return true
}

func GetISS(server Server) (string, error) {
	base, baseErr := server.GetBase()
	if baseErr != nil {
		return "", &types.WrappedErrorMessage{Message: "failed getting server ISS", Err: baseErr}
	}
	// We have already ensured that the base URL ends with a /
	return base.URL, nil
}

func GetOAuthURL(server Server, name string) (string, error) {
	iss, issErr := GetISS(server)
	if issErr != nil {
		return "", issErr
	}
	return server.GetOAuth().GetAuthURL(name, iss, server.GetTemplateAuth())
}

func OAuthExchange(server Server) error {
	return server.GetOAuth().Exchange()
}

func GetHeaderToken(server Server) string {
	return server.GetOAuth().Token.Access
}

func MarkTokenExpired(server Server) {
	server.GetOAuth().Token.ExpiredTimestamp = util.GetCurrentTime()
}

func EnsureTokens(server Server) error {
	ensureErr := server.GetOAuth().EnsureTokens()
	if ensureErr != nil {
		return &types.WrappedErrorMessage{Message: "failed ensuring server tokens", Err: ensureErr}
	}
	return nil
}

func NeedsRelogin(server Server) bool {
	return EnsureTokens(server) != nil
}

func CancelOAuth(server Server) {
	server.GetOAuth().Cancel()
}

func (profile *ServerProfile) supportsProtocol(protocol string) bool {
	for _, proto := range profile.VPNProtoList {
		if proto == protocol {
			return true
		}
	}
	return false
}

func (profile *ServerProfile) supportsWireguard() bool {
	return profile.supportsProtocol("wireguard")
}

func (profile *ServerProfile) supportsOpenVPN() bool {
	return profile.supportsProtocol("openvpn")
}

func getCurrentProfile(server Server) (*ServerProfile, error) {
	errorMessage := "failed getting current profile"
	base, baseErr := server.GetBase()

	if baseErr != nil {
		return nil, &types.WrappedErrorMessage{Message: errorMessage, Err: baseErr}
	}
	profileID := base.Profiles.Current
	for _, profile := range base.Profiles.Info.ProfileList {
		if profile.ID == profileID {
			return &profile, nil
		}
	}

	return nil, &types.WrappedErrorMessage{
		Message: errorMessage,
		Err:     &ServerGetCurrentProfileNotFoundError{ProfileID: profileID},
	}
}

func wireguardGetConfig(server Server, preferTCP bool, supportsOpenVPN bool) (string, string, error) {
	errorMessage := "failed getting server WireGuard configuration"
	base, baseErr := server.GetBase()

	if baseErr != nil {
		return "", "", &types.WrappedErrorMessage{Message: errorMessage, Err: baseErr}
	}

	profile_id := base.Profiles.Current
	wireguardKey, wireguardErr := wireguard.GenerateKey()

	if wireguardErr != nil {
		return "", "", &types.WrappedErrorMessage{Message: errorMessage, Err: wireguardErr}
	}

	wireguardPublicKey := wireguardKey.PublicKey().String()
	config, content, expires, configErr := APIConnectWireguard(
		server,
		profile_id,
		wireguardPublicKey,
		preferTCP,
		supportsOpenVPN,
	)

	if configErr != nil {
		return "", "", &types.WrappedErrorMessage{Message: errorMessage, Err: configErr}
	}

	// Store start and end time
	base.StartTime = util.GetCurrentTime()
	base.EndTime = expires

	if content == "wireguard" {
		// This needs the go code a way to identify a connection
		// Use the uuid of the connection e.g. on Linux
		// This needs the client code to call the go code

		config = wireguard.ConfigAddKey(config, wireguardKey)
	}

	return config, content, nil
}

func openVPNGetConfig(server Server, preferTCP bool) (string, string, error) {
	errorMessage := "failed getting server OpenVPN configuration"
	base, baseErr := server.GetBase()

	if baseErr != nil {
		return "", "", &types.WrappedErrorMessage{Message: errorMessage, Err: baseErr}
	}
	profile_id := base.Profiles.Current
	configOpenVPN, expires, configErr := APIConnectOpenVPN(server, profile_id, preferTCP)

	// Store start and end time
	base.StartTime = util.GetCurrentTime()
	base.EndTime = expires

	if configErr != nil {
		return "", "", &types.WrappedErrorMessage{Message: errorMessage, Err: configErr}
	}

	return configOpenVPN, "openvpn", nil
}

func HasValidProfile(server Server) (bool, error) {
	errorMessage := "failed has valid profile check"

	// Get new profiles using the info call
	// This does not override the current profile
	infoErr := APIInfo(server)
	if infoErr != nil {
		return false, &types.WrappedErrorMessage{Message: errorMessage, Err: infoErr}
	}

	base, baseErr := server.GetBase()
	if baseErr != nil {
		return false, &types.WrappedErrorMessage{Message: errorMessage, Err: baseErr}
	}

	// If there was a profile chosen and it doesn't exist anymore, reset it
	if base.Profiles.Current != "" {
		_, existsProfileErr := getCurrentProfile(server)
		if existsProfileErr != nil {
			base.Profiles.Current = ""
		}
	}

	// Set the current profile if there is only one profile or profile is already selected
	if len(base.Profiles.Info.ProfileList) == 1 || base.Profiles.Current != "" {
		// Set the first profile if none is selected
		if base.Profiles.Current == "" {
			base.Profiles.Current = base.Profiles.Info.ProfileList[0].ID
		}
		return true, nil
	}

	return false, nil
}

func GetConfig(server Server, preferTCP bool) (string, string, error) {
	errorMessage := "failed getting an OpenVPN/WireGuard configuration"

	profile, profileErr := getCurrentProfile(server)
	if profileErr != nil {
		return "", "", &types.WrappedErrorMessage{Message: errorMessage, Err: profileErr}
	}

	supportsOpenVPN := profile.supportsOpenVPN()
	supportsWireguard := profile.supportsWireguard()

	var config string
	var configType string
	var configErr error

	if supportsWireguard {
		// A wireguard connect call needs to generate a wireguard key and add it to the config
		// Also the server could send back an OpenVPN config if it supports OpenVPN
		config, configType, configErr = wireguardGetConfig(server, preferTCP, supportsOpenVPN)
	} else {
		config, configType, configErr = openVPNGetConfig(server, preferTCP)
	}

	if configErr != nil {
		return "", "", &types.WrappedErrorMessage{Message: errorMessage, Err: configErr}
	}

	return config, configType, nil
}

func Disconnect(server Server) {
	APIDisconnect(server)
}

type ServerGetCurrentProfileNotFoundError struct {
	ProfileID string
}

func (e *ServerGetCurrentProfileNotFoundError) Error() string {
	return fmt.Sprintf("failed to get current profile, profile with ID: %s not found", e.ProfileID)
}

type ServerGetConfigForceTCPError struct{}

func (e *ServerGetConfigForceTCPError) Error() string {
	return "failed to get config, prefer TCP is on but the server does not support OpenVPN"
}

type ServerEnsureServerEmptyURLError struct{}

func (e *ServerEnsureServerEmptyURLError) Error() string {
	return "failed ensuring server, empty url provided"
}

type ServerGetCurrentNoMapError struct{}

func (e *ServerGetCurrentNoMapError) Error() string {
	return "failed getting current server, no servers available"
}

type ServerGetCurrentNotFoundError struct{}

func (e *ServerGetCurrentNotFoundError) Error() string {
	return "failed getting current server, not found"
}
