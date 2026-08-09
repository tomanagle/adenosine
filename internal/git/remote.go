package git

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/adenosine-dev/adenosine/internal/repository"
)

var (
	// ErrRemoteInput indicates malformed or unsafe remote fetch input.
	ErrRemoteInput = errors.New("invalid remote Git input")
	// ErrRemoteAddress indicates that DNS returned a non-public address.
	ErrRemoteAddress = errors.New("remote Git endpoint resolves to a prohibited address")
	// ErrHeadMismatch indicates that the fetched branch does not match its declared head.
	ErrHeadMismatch = errors.New("remote Git head does not match expected SHA")
	// ErrRefConflict indicates that the controlled ref changed since the caller observed it.
	ErrRefConflict = errors.New("remote Git destination ref changed")
)

// Resolver is the DNS boundary used by remote Git fetches.
type Resolver interface {
	LookupIP(context.Context, string, string) ([]net.IP, error)
}

// RemoteFetch identifies one immutable source head and its controlled local destination.
// PriorHead is empty when the destination must not exist, otherwise it is the observed
// destination SHA used for compare-and-swap promotion.
type RemoteFetch struct {
	SourceURL    string
	SourceBranch string
	ExpectedHead string
	Destination  string
	PriorHead    string
}

// FetchRemote securely fetches a declared HTTPS branch into a controlled pull ref.
func (service *Service) FetchRemote(ctx context.Context, id repository.ID, request RemoteFetch) (resultErr error) {
	endpoint, err := validateRemoteFetch(request)
	if err != nil {
		return err
	}
	addresses, err := service.resolveRemote(ctx, endpoint.Hostname())
	if err != nil {
		return err
	}
	repositoryPath, err := service.repositoryPath(ctx, id)
	if err != nil {
		return err
	}
	output, err := service.runBounded(ctx, maxMetadataOutput, []string{"--git-dir=" + repositoryPath, "rev-parse", "--show-object-format"})
	if err != nil {
		return fmt.Errorf("determine object format: %w", err)
	}
	objectFormat, err := singleLine(output)
	if err != nil {
		return fmt.Errorf("parse object format: %w", err)
	}
	expectedLength := 40
	if objectFormat == "sha256" {
		expectedLength = 64
	} else if objectFormat != "sha1" {
		return fmt.Errorf("unsupported object format %q", objectFormat)
	}
	if len(request.ExpectedHead) != expectedLength || (request.PriorHead != "" && len(request.PriorHead) != expectedLength) {
		return fmt.Errorf("SHA length does not match %s repository: %w", objectFormat, ErrRemoteInput)
	}

	quarantine, err := randomQuarantineRef()
	if err != nil {
		return fmt.Errorf("create quarantine ref: %w", err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		cleanupErr := service.runner.run(cleanupCtx, []string{"--git-dir=" + repositoryPath, "update-ref", "-d", quarantine}, nil, nil)
		if cleanupErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("delete quarantine ref: %w", cleanupErr))
		}
	}()

	args := hardenedFetchArgs(repositoryPath, endpoint, addresses, service.runner.httpCAInfo)
	args = append(args,
		"fetch", "--no-tags", "--no-write-fetch-head", "--no-recurse-submodules", "--force",
		request.SourceURL,
		"+refs/heads/"+request.SourceBranch+":"+quarantine,
	)
	if err := service.runner.run(ctx, args, nil, nil); err != nil {
		return fmt.Errorf("fetch remote branch: %w", err)
	}
	fetched, err := service.resolve(ctx, repositoryPath, quarantine)
	if err != nil {
		return fmt.Errorf("resolve fetched head: %w", err)
	}
	objectType, err := service.objectProperty(ctx, repositoryPath, "-t", fetched)
	if err != nil {
		return fmt.Errorf("inspect fetched head: %w", err)
	}
	if objectType != "commit" || fetched != request.ExpectedHead {
		return ErrHeadMismatch
	}

	destination, err := controlledPullRef(request.Destination)
	if err != nil {
		return err
	}
	prior := request.PriorHead
	if prior == "" {
		prior = strings.Repeat("0", expectedLength)
	}
	if err := service.runner.run(ctx, []string{"--git-dir=" + repositoryPath, "update-ref", destination, fetched, prior}, nil, nil); err != nil {
		return fmt.Errorf("promote fetched head: %w: %w", ErrRefConflict, err)
	}
	return nil
}

func validateRemoteFetch(request RemoteFetch) (*url.URL, error) {
	if err := validateRemoteSHA(request.ExpectedHead); err != nil {
		return nil, err
	}
	if request.PriorHead != "" {
		if err := validateRemoteSHA(request.PriorHead); err != nil {
			return nil, err
		}
	}
	if _, err := controlledPullRef(request.Destination); err != nil {
		return nil, err
	}
	if err := validateBranch(request.SourceBranch); err != nil {
		return nil, err
	}
	endpoint, err := url.Parse(request.SourceURL)
	if err != nil || endpoint.Scheme != "https" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Fragment != "" || endpoint.Opaque != "" {
		return nil, fmt.Errorf("source URL must be canonical HTTPS: %w", ErrRemoteInput)
	}
	if endpoint.Hostname() == "" || endpoint.RawPath != "" || endpoint.Path == "" || endpoint.Path[0] != '/' || strings.Contains(endpoint.Path, "//") || strings.HasSuffix(endpoint.Path, "/") {
		return nil, fmt.Errorf("source URL must have a canonical host and path: %w", ErrRemoteInput)
	}
	for _, component := range strings.Split(strings.TrimPrefix(endpoint.Path, "/"), "/") {
		if component == "" || component == "." || component == ".." {
			return nil, fmt.Errorf("source URL has an ambiguous path: %w", ErrRemoteInput)
		}
		for _, character := range component {
			if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || strings.ContainsRune("-._~:", character)) {
				return nil, fmt.Errorf("source URL path is not canonical: %w", ErrRemoteInput)
			}
		}
	}
	hostname := endpoint.Hostname()
	if hostname != strings.ToLower(hostname) || strings.Contains(hostname, "%") || !canonicalHostname(hostname) {
		return nil, fmt.Errorf("source URL host is not canonical: %w", ErrRemoteInput)
	}
	port := endpoint.Port()
	if port != "" {
		parsed, parseErr := strconv.Atoi(port)
		if parseErr != nil || parsed < 1 || parsed > 65535 || strconv.Itoa(parsed) != port || parsed == 443 {
			return nil, fmt.Errorf("source URL port is not canonical: %w", ErrRemoteInput)
		}
	}
	canonicalHost := hostname
	if net.ParseIP(hostname) != nil && strings.Contains(hostname, ":") {
		canonicalHost = "[" + hostname + "]"
	}
	if port != "" {
		canonicalHost = net.JoinHostPort(hostname, port)
	}
	if endpoint.Host != canonicalHost || endpoint.String() != request.SourceURL {
		return nil, fmt.Errorf("source URL representation is not canonical: %w", ErrRemoteInput)
	}
	return endpoint, nil
}

// ControlledHead returns the current commit at a controlled pull destination.
// A destination that has not been fetched returns an empty SHA without error.
func (service *Service) ControlledHead(ctx context.Context, id repository.ID, destination string) (string, error) {
	ref, err := controlledPullRef(destination)
	if err != nil {
		return "", err
	}
	repositoryPath, err := service.repositoryPath(ctx, id)
	if err != nil {
		return "", err
	}
	sha, err := service.resolve(ctx, repositoryPath, ref)
	if errors.Is(err, ErrObjectNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("resolve controlled pull head: %w", err)
	}
	if err := validateFullSHA(sha); err != nil {
		return "", fmt.Errorf("parse controlled pull head: %w", err)
	}
	return sha, nil
}

func controlledPullRef(destination string) (string, error) {
	if destination == "" || len(destination) > 128 {
		return "", fmt.Errorf("invalid destination: %w", ErrRemoteInput)
	}
	for _, character := range destination {
		if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '-') {
			return "", fmt.Errorf("invalid destination: %w", ErrRemoteInput)
		}
	}
	return "refs/adenosine/pull/" + destination + "/head", nil
}

func canonicalHostname(hostname string) bool {
	if address, err := netip.ParseAddr(hostname); err == nil {
		return address.String() == hostname
	}
	if len(hostname) > 253 || strings.Contains(hostname, ":") || strings.Trim(hostname, "0123456789.") == "" {
		return false
	}
	for _, label := range strings.Split(hostname, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if !((character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '-') {
				return false
			}
		}
	}
	return true
}

func validateBranch(branch string) error {
	if branch == "" || branch == "@" || len(branch) > 1024 || strings.HasPrefix(branch, "-") || strings.HasPrefix(branch, "/") || strings.HasSuffix(branch, "/") || strings.Contains(branch, "..") || strings.Contains(branch, "//") || strings.Contains(branch, "@{") || strings.HasSuffix(branch, ".") || strings.HasSuffix(branch, ".lock") {
		return fmt.Errorf("invalid source branch: %w", ErrRemoteInput)
	}
	for _, component := range strings.Split(branch, "/") {
		if strings.HasPrefix(component, ".") || strings.HasSuffix(component, ".lock") {
			return fmt.Errorf("invalid source branch: %w", ErrRemoteInput)
		}
	}
	for _, character := range branch {
		if character <= 0x20 || character == 0x7f || strings.ContainsRune("~^:?*[\\", character) {
			return fmt.Errorf("invalid source branch: %w", ErrRemoteInput)
		}
	}
	return nil
}

func validateRemoteSHA(sha string) error {
	if len(sha) != 40 && len(sha) != 64 {
		return fmt.Errorf("SHA must be full length: %w", ErrRemoteInput)
	}
	for _, character := range sha {
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return fmt.Errorf("SHA must be lowercase hexadecimal: %w", ErrRemoteInput)
		}
	}
	return nil
}

func (service *Service) resolveRemote(ctx context.Context, hostname string) ([]net.IP, error) {
	addresses, err := service.resolver.LookupIP(ctx, "ip", hostname)
	if err != nil {
		return nil, fmt.Errorf("resolve remote Git endpoint: %w", err)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("resolve remote Git endpoint: no addresses")
	}
	for _, address := range addresses {
		if !service.allowIP(address) {
			return nil, fmt.Errorf("%s: %w", address, ErrRemoteAddress)
		}
	}
	return addresses, nil
}

var prohibitedPrefixes = func() []netip.Prefix {
	values := []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "192.175.48.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/23", "2001:db8::/32", "2002::/16", "2620:4f:8000::/48", "3fff::/20", "5f00::/16"}
	result := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		result = append(result, netip.MustParsePrefix(value))
	}
	return result
}()

func isPublicIP(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified() {
		return false
	}
	for _, prefix := range prohibitedPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func hardenedFetchArgs(repositoryPath string, endpoint *url.URL, addresses []net.IP, caInfo string) []string {
	port := endpoint.Port()
	if port == "" {
		port = "443"
	}
	scope := "http." + endpoint.String() + "."
	args := []string{
		"--git-dir=" + repositoryPath,
		"-c", "protocol.allow=never",
		"-c", "protocol.https.allow=always",
		"-c", "credential.helper=",
		"-c", "core.askPass=",
		"-c", "http.sslVerify=true",
		"-c", scope + "sslVerify=true",
		"-c", "http.followRedirects=false",
		"-c", scope + "followRedirects=false",
		"-c", "http.proxy=",
		"-c", scope + "proxy=",
		"-c", "http.extraHeader=",
		"-c", scope + "extraHeader=",
		"-c", "http.cookieFile=",
		"-c", scope + "cookieFile=",
		"-c", "http.saveCookies=false",
		"-c", scope + "saveCookies=false",
	}
	if caInfo != "" {
		args = append(args, "-c", "http.sslCAInfo="+caInfo, "-c", scope+"sslCAInfo="+caInfo)
	}
	for _, address := range addresses {
		text := address.String()
		if address.To4() == nil {
			text = "[" + text + "]"
		}
		args = append(args, "-c", "http.curloptResolve="+endpoint.Hostname()+":"+port+":"+text)
		args = append(args, "-c", scope+"curloptResolve="+endpoint.Hostname()+":"+port+":"+text)
	}
	return args
}

func randomQuarantineRef() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return "refs/adenosine/quarantine/" + hex.EncodeToString(value), nil
}
