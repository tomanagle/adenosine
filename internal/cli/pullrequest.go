package cli

import (
	"context"
	"flag"
	"fmt"

	generated "github.com/adenosine-dev/adenosine/api/generated/go"
)

func (runner *runner) pullRequest(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: pr requires create, checkout, view, or merge", ErrUsage)
	}
	switch args[0] {
	case "create":
		return runner.createPullRequest(ctx, args[1:])
	case "checkout":
		return runner.checkoutPullRequest(ctx, args[1:])
	case "view":
		return runner.viewPullRequest(ctx, args[1:])
	case "merge":
		return runner.mergePullRequest(ctx, args[1:])
	default:
		return fmt.Errorf("%w: unknown pr command %q", ErrUsage, args[0])
	}
}

func (runner *runner) createPullRequest(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("pr create", flag.ContinueOnError)
	flags.SetOutput(runner.stderr)
	addCommonFlags(flags)
	source := flags.String("source-repo", "", "source OWNER/REPO")
	target := flags.String("target-repo", "", "target OWNER/REPO")
	sourceBranch := flags.String("source-branch", "", "source branch")
	targetBranch := flags.String("target-branch", "main", "target branch")
	headSHA := flags.String("head", "", "immutable source head SHA")
	title := flags.String("title", "", "pull request title")
	body := flags.String("body", "", "pull request body")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", ErrUsage, err)
	}
	if flags.NArg() != 0 || *source == "" || *target == "" || *sourceBranch == "" || *targetBranch == "" || *headSHA == "" || *title == "" {
		return fmt.Errorf("%w: pr create requires source/target repositories, branches, head, and title", ErrUsage)
	}
	client, jsonOutput, err := runner.client(flags)
	if err != nil {
		return err
	}
	sourceRepository, err := runner.resolveRepository(ctx, client, *source)
	if err != nil {
		return fmt.Errorf("resolve source repository: %w", err)
	}
	targetRepository, err := runner.resolveRepository(ctx, client, *target)
	if err != nil {
		return fmt.Errorf("resolve target repository: %w", err)
	}
	response, err := client.CreatePullRequestWithResponse(ctx, &generated.CreatePullRequestParams{}, generated.CreatePullRequestRequest{
		SourceRepositoryUri: *sourceRepository.Uri, TargetRepositoryUri: *targetRepository.Uri,
		SourceBranch: *sourceBranch, TargetBranch: *targetBranch, HeadSha: *headSHA, Title: *title, Body: *body,
	})
	if err != nil {
		return fmt.Errorf("create pull request: %w", err)
	}
	if response.JSON202 == nil {
		return responseError(response.StatusCode(), response.Body)
	}
	if jsonOutput {
		return writeJSON(runner.stdout, response.JSON202)
	}
	_, err = fmt.Fprintln(runner.stdout, response.JSON202.PullRequest.Uri)
	return err
}

func (runner *runner) viewPullRequest(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("pr view", flag.ContinueOnError)
	flags.SetOutput(runner.stderr)
	addCommonFlags(flags)
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", ErrUsage, err)
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("%w: pr view requires PULL_REQUEST_URI", ErrUsage)
	}
	client, jsonOutput, err := runner.client(flags)
	if err != nil {
		return err
	}
	response, err := client.GetPullRequestWithResponse(ctx, &generated.GetPullRequestParams{PullRequestUri: flags.Arg(0)})
	if err != nil {
		return fmt.Errorf("get pull request: %w", err)
	}
	if response.JSON200 == nil {
		return responseError(response.StatusCode(), response.Body)
	}
	if jsonOutput {
		return writeJSON(runner.stdout, response.JSON200)
	}
	value := response.JSON200
	_, err = fmt.Fprintf(runner.stdout, "%s [%s]\n%s\n%s → %s\n%s\n", value.Title, value.State, value.Uri, value.SourceBranch, value.TargetBranch, value.Body)
	return err
}

func (runner *runner) checkoutPullRequest(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("pr checkout", flag.ContinueOnError)
	flags.SetOutput(runner.stderr)
	flags.String("host", "", "Adenosine server URL")
	branch := flags.String("branch", "", "local branch name")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", ErrUsage, err)
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("%w: pr checkout requires PULL_REQUEST_URI", ErrUsage)
	}
	client, _, err := runner.client(flags)
	if err != nil {
		return err
	}
	response, err := client.GetPullRequestCheckoutWithResponse(ctx, &generated.GetPullRequestCheckoutParams{PullRequestUri: flags.Arg(0)})
	if err != nil {
		return fmt.Errorf("resolve pull request checkout: %w", err)
	}
	if response.JSON200 == nil {
		return responseError(response.StatusCode(), response.Body)
	}
	localBranch := *branch
	if localBranch == "" {
		localBranch = response.JSON200.SourceBranch
	}
	if err := runner.git.Run(ctx, "fetch", "--no-tags", response.JSON200.GitHttpsUrl, response.JSON200.HeadSha); err != nil {
		return fmt.Errorf("fetch pull request: %w", err)
	}
	if err := runner.git.Run(ctx, "checkout", "-B", localBranch, "FETCH_HEAD"); err != nil {
		return fmt.Errorf("checkout pull request: %w", err)
	}
	return nil
}

func (runner *runner) mergePullRequest(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("pr merge", flag.ContinueOnError)
	flags.SetOutput(runner.stderr)
	addCommonFlags(flags)
	strategy := flags.String("strategy", "merge-commit", "merge-commit or squash")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", ErrUsage, err)
	}
	if flags.NArg() != 1 || (*strategy != "merge-commit" && *strategy != "squash") {
		return fmt.Errorf("%w: pr merge requires PULL_REQUEST_URI and a valid strategy", ErrUsage)
	}
	client, jsonOutput, err := runner.client(flags)
	if err != nil {
		return err
	}
	response, err := client.MergePullRequestWithResponse(ctx, &generated.MergePullRequestParams{}, generated.MergePullRequestRequest{
		PullRequestUri: flags.Arg(0), Strategy: generated.MergePullRequestRequestStrategy(*strategy),
	})
	if err != nil {
		return fmt.Errorf("merge pull request: %w", err)
	}
	if response.JSON200 == nil {
		return responseError(response.StatusCode(), response.Body)
	}
	if jsonOutput {
		return writeJSON(runner.stdout, response.JSON200)
	}
	_, err = fmt.Fprintln(runner.stdout, response.JSON200.MergeCommitSha)
	return err
}
