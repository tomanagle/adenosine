package cli

import (
	"context"
	"flag"
	"fmt"

	generated "github.com/adenosine-dev/adenosine/api/generated/go"
)

func (runner *runner) issue(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: issue requires create, list, or view", ErrUsage)
	}
	switch args[0] {
	case "create":
		return runner.createIssue(ctx, args[1:])
	case "list":
		return runner.listIssues(ctx, args[1:])
	case "view":
		return runner.viewIssue(ctx, args[1:])
	default:
		return fmt.Errorf("%w: unknown issue command %q", ErrUsage, args[0])
	}
}

func (runner *runner) listIssues(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("issue list", flag.ContinueOnError)
	flags.SetOutput(runner.stderr)
	addCommonFlags(flags)
	repositoryName := flags.String("repo", "", "target OWNER/REPO")
	limit := flags.Int("limit", 30, "page size between 1 and 100")
	cursor := flags.String("cursor", "", "opaque cursor returned by the previous page")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", ErrUsage, err)
	}
	if flags.NArg() != 0 || *repositoryName == "" || *limit < 1 || *limit > 100 {
		return fmt.Errorf("%w: issue list requires --repo and a limit between 1 and 100", ErrUsage)
	}
	client, jsonOutput, err := runner.client(flags)
	if err != nil {
		return err
	}
	repository, err := runner.resolveRepository(ctx, client, *repositoryName)
	if err != nil {
		return fmt.Errorf("resolve repository: %w", err)
	}
	pageLimit := generated.Limit(*limit)
	params := generated.GetIssuesParams{RepositoryUri: *repository.Uri, Limit: &pageLimit}
	if *cursor != "" {
		value := generated.Cursor(*cursor)
		params.Cursor = &value
	}
	response, err := client.GetIssuesWithResponse(ctx, &params)
	if err != nil {
		return fmt.Errorf("list issues: %w", err)
	}
	if response.JSON200 == nil {
		return responseError(response.StatusCode(), response.Body)
	}
	if jsonOutput {
		return writeJSON(runner.stdout, response.JSON200)
	}
	for _, value := range response.JSON200.Items {
		if _, err := fmt.Fprintf(runner.stdout, "%s\t%s [%s]\n", value.Uri, value.Title, value.State); err != nil {
			return err
		}
	}
	if response.JSON200.Page.NextCursor != nil {
		_, err = fmt.Fprintf(runner.stdout, "next cursor: %s\n", *response.JSON200.Page.NextCursor)
	}
	return err
}

func (runner *runner) createIssue(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("issue create", flag.ContinueOnError)
	flags.SetOutput(runner.stderr)
	addCommonFlags(flags)
	repositoryName := flags.String("repo", "", "target OWNER/REPO")
	title := flags.String("title", "", "issue title")
	body := flags.String("body", "", "issue body")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", ErrUsage, err)
	}
	if flags.NArg() != 0 || *repositoryName == "" || *title == "" {
		return fmt.Errorf("%w: issue create requires --repo and --title", ErrUsage)
	}
	client, jsonOutput, err := runner.client(flags)
	if err != nil {
		return err
	}
	repository, err := runner.resolveRepository(ctx, client, *repositoryName)
	if err != nil {
		return fmt.Errorf("resolve repository: %w", err)
	}
	response, err := client.CreateIssueWithResponse(ctx, &generated.CreateIssueParams{}, generated.CreateIssueRequest{RepositoryUri: *repository.Uri, Title: *title, Body: *body})
	if err != nil {
		return fmt.Errorf("create issue: %w", err)
	}
	if response.JSON202 == nil {
		return responseError(response.StatusCode(), response.Body)
	}
	if jsonOutput {
		return writeJSON(runner.stdout, response.JSON202)
	}
	_, err = fmt.Fprintln(runner.stdout, response.JSON202.Issue.Uri)
	return err
}

func (runner *runner) viewIssue(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("issue view", flag.ContinueOnError)
	flags.SetOutput(runner.stderr)
	addCommonFlags(flags)
	repositoryName := flags.String("repo", "", "target OWNER/REPO")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", ErrUsage, err)
	}
	if flags.NArg() != 1 || *repositoryName == "" {
		return fmt.Errorf("%w: issue view requires --repo and ISSUE_URI", ErrUsage)
	}
	client, jsonOutput, err := runner.client(flags)
	if err != nil {
		return err
	}
	repository, err := runner.resolveRepository(ctx, client, *repositoryName)
	if err != nil {
		return fmt.Errorf("resolve repository: %w", err)
	}
	response, err := client.GetIssueWithResponse(ctx, &generated.GetIssueParams{RepositoryUri: *repository.Uri, IssueUri: flags.Arg(0)})
	if err != nil {
		return fmt.Errorf("get issue: %w", err)
	}
	if response.JSON200 == nil {
		return responseError(response.StatusCode(), response.Body)
	}
	if jsonOutput {
		return writeJSON(runner.stdout, response.JSON200)
	}
	value := response.JSON200
	_, err = fmt.Fprintf(runner.stdout, "%s [%s]\n%s\n%s\n", value.Title, value.State, value.Uri, value.Body)
	return err
}
