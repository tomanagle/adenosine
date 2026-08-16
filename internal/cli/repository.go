package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"

	generated "github.com/adenosine-dev/adenosine/api/generated/go"
)

func (runner *runner) repo(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: repo requires create or view", ErrUsage)
	}
	switch args[0] {
	case "create":
		return runner.createRepository(ctx, args[1:])
	case "view":
		return runner.viewRepository(ctx, args[1:])
	default:
		return fmt.Errorf("%w: unknown repo command %q", ErrUsage, args[0])
	}
}

func (runner *runner) createRepository(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("repo create", flag.ContinueOnError)
	flags.SetOutput(runner.stderr)
	addCommonFlags(flags)
	description := flags.String("description", "", "repository description")
	visibility := flags.String("visibility", "public", "public or private")
	organization := flags.String("organization", "", "destination organization")
	defaultBranch := flags.String("default-branch", "main", "default branch")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", ErrUsage, err)
	}
	if flags.NArg() != 1 || (*visibility != "public" && *visibility != "private") {
		return fmt.Errorf("%w: repo create requires SLUG and a valid --visibility", ErrUsage)
	}
	client, jsonOutput, err := runner.client(flags)
	if err != nil {
		return err
	}
	visibilityValue := generated.CreateRepositoryRequestVisibility(*visibility)
	body := generated.CreateRepositoryRequest{Slug: generated.RepositorySlug(flags.Arg(0)), DefaultBranch: defaultBranch, Visibility: &visibilityValue}
	if *description != "" {
		body.Description = description
	}
	if *organization != "" {
		value := generated.OrganizationSlug(*organization)
		body.Organization = &value
	}
	response, err := client.CreateRepositoryWithResponse(ctx, &generated.CreateRepositoryParams{}, body)
	if err != nil {
		return fmt.Errorf("create repository: %w", err)
	}
	if response.JSON201 == nil {
		return responseError(response.StatusCode(), response.Body)
	}
	if jsonOutput {
		return writeJSON(runner.stdout, response.JSON201)
	}
	_, err = fmt.Fprintf(runner.stdout, "%s/%s\n%s\n", repositoryOwner(*response.JSON201), response.JSON201.Slug, response.JSON201.Hosting.GitHttpsUrl)
	return err
}

func (runner *runner) viewRepository(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("repo view", flag.ContinueOnError)
	flags.SetOutput(runner.stderr)
	addCommonFlags(flags)
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", ErrUsage, err)
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("%w: repo view requires OWNER/REPO", ErrUsage)
	}
	owner, slug, err := splitRepository(flags.Arg(0))
	if err != nil {
		return err
	}
	client, jsonOutput, err := runner.client(flags)
	if err != nil {
		return err
	}
	response, err := client.GetRepositoryWithResponse(ctx, owner, generated.RepositorySlug(slug))
	if err != nil {
		return fmt.Errorf("get repository: %w", err)
	}
	if response.JSON200 == nil {
		return responseError(response.StatusCode(), response.Body)
	}
	if jsonOutput {
		return writeJSON(runner.stdout, response.JSON200)
	}
	value := response.JSON200
	_, err = fmt.Fprintf(runner.stdout, "%s/%s\n%s\n%s\n", repositoryOwner(*value), value.Slug, value.Hosting.WebUrl, value.Hosting.GitHttpsUrl)
	return err
}

func repositoryOwner(value generated.Repository) string {
	if value.Owner.OrganizationSlug != nil {
		return string(*value.Owner.OrganizationSlug)
	}
	if value.Owner.Handle != nil {
		return *value.Owner.Handle
	}
	return value.Owner.Did
}

func (runner *runner) resolveRepository(ctx context.Context, client *generated.ClientWithResponses, value string) (*generated.Repository, error) {
	owner, slug, err := splitRepository(value)
	if err != nil {
		return nil, err
	}
	response, err := client.GetRepositoryWithResponse(ctx, owner, generated.RepositorySlug(slug))
	if err != nil {
		return nil, err
	}
	if response.JSON200 == nil {
		return nil, responseError(response.StatusCode(), response.Body)
	}
	if response.JSON200.Uri == nil || *response.JSON200.Uri == "" {
		return nil, errors.New("repository has no portable URI")
	}
	return response.JSON200, nil
}
