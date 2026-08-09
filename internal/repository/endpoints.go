package repository

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// Endpoints constructs current repository routes from instance configuration.
type Endpoints struct {
	baseURL string
	sshHost string
	sshPort uint16
}

// Must constructs required publication endpoint configuration or panics at startup.
func Must(baseURL, sshHost string, sshPort uint16) *Endpoints {
	endpoints, err := buildEndpoints(baseURL, sshHost, sshPort)
	if err != nil {
		panic(err)
	}
	return endpoints
}

func buildEndpoints(baseURL, sshHost string, sshPort uint16) (*Endpoints, error) {
	base, err := url.Parse(baseURL)
	if err != nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Scheme != "http" && base.Scheme != "https") {
		return nil, errors.New("repository base URL is invalid")
	}
	if !validSSHHost(sshHost) || sshPort == 0 {
		return nil, errors.New("repository SSH endpoint is invalid")
	}
	return &Endpoints{baseURL: strings.TrimSuffix(base.String(), "/"), sshHost: sshHost, sshPort: sshPort}, nil
}

// For returns web, HTTPS Git, and SSH Git URLs using immutable owner identity.
func (endpoints *Endpoints) For(repository Repository) (string, string, string) {
	path := "/" + repository.OwnerDID + "/" + repository.Slug
	web := endpoints.baseURL + path
	authority := endpoints.sshHost
	if endpoints.sshPort != 22 {
		authority = net.JoinHostPort(endpoints.sshHost, strconv.Itoa(int(endpoints.sshPort)))
	} else if strings.Contains(endpoints.sshHost, ":") {
		authority = "[" + endpoints.sshHost + "]"
	}
	return web, web + ".git", "ssh://git@" + authority + path + ".git"
}

func validSSHHost(host string) bool {
	if host == "" || strings.TrimSpace(host) != host || strings.ContainsAny(host, "/@") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return true
	}
	parsed, err := url.Parse("ssh://" + host)
	return err == nil && parsed.Hostname() == host && parsed.Port() == ""
}
