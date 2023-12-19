package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v56/github"
)

type GitHubAppClient struct {
	*github.Client
}

func NewGitHubAppClient(cfg Config) (*GitHubAppClient, error) {
	itr, err := ghinstallation.NewAppsTransport(http.DefaultTransport, cfg.GitHubAppID, cfg.GitHubAppPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("couldn't get transport key: %w", err)
	}

	client := github.NewClient(&http.Client{Transport: itr})
	return &GitHubAppClient{client}, nil
}

func (ghClient *GitHubAppClient) GetInstallationClient(ctx context.Context, owner, repo string) (*github.Client, error) {
	install, _, err := ghClient.Apps.FindRepositoryInstallation(ctx, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("couldn't find repo installation: %w", err)
	}

	token, _, err := ghClient.Apps.CreateInstallationToken(ctx, install.GetID(), &github.InstallationTokenOptions{})
	if err != nil {
		return nil, fmt.Errorf("couldn't create installation token: %w", err)
	}

	return github.NewClient(nil).WithAuthToken(token.GetToken()), nil
}
