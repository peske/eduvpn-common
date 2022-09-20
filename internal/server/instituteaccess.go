package server

import (
	"fmt"

	"github.com/eduvpn/eduvpn-common/internal/oauth"
	"github.com/eduvpn/eduvpn-common/internal/types"
)

// An instute access server
type InstituteAccessServer struct {
	// An instute access server has its own OAuth
	OAuth oauth.OAuth `json:"oauth"`

	// Embed the server base
	Base ServerBase `json:"base"`
}

type InstituteAccessServers struct {
	Map        map[string]*InstituteAccessServer `json:"map"`
	CurrentURL string                            `json:"current_url"`
}

func (servers *Servers) RemoveInstituteAccess(url string) {
	servers.InstituteServers.Remove(url)
}

func (servers *InstituteAccessServers) Remove(url string) {
	// Reset the current url
	if servers.CurrentURL == url {
		servers.CurrentURL = ""
	}

	// Delete the url from the map
	delete(servers.Map, url)
}

// For an institute, we can simply get the OAuth
func (institute *InstituteAccessServer) GetOAuth() *oauth.OAuth {
	return &institute.OAuth
}

func (institute *InstituteAccessServer) GetTemplateAuth() func(string) string {
	return func(authURL string) string {
		return authURL
	}
}

func (institute *InstituteAccessServer) GetBase() (*ServerBase, error) {
	return &institute.Base, nil
}

func (institute *InstituteAccessServer) init(
	url string,
	displayName map[string]string,
	serverType string,
	supportContact []string,
) error {
	errorMessage := fmt.Sprintf("failed initializing institute server %s", url)
	institute.Base.URL = url
	institute.Base.DisplayName = displayName
	institute.Base.SupportContact = supportContact
	institute.Base.Type = serverType
	endpoints, endpointsErr := APIGetEndpoints(url)
	if endpointsErr != nil {
		return &types.WrappedErrorMessage{Message: errorMessage, Err: endpointsErr}
	}
	institute.OAuth.Init(endpoints.API.V3.Authorization, endpoints.API.V3.Token)
	institute.Base.Endpoints = *endpoints
	return nil
}
