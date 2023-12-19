package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/cbrgm/githubevents/githubevents"
	"github.com/google/go-github/v56/github"
	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	WebhookSecret       string     `envconfig:"WEBHOOK_SECRET" required:"true"`
	GitHubAppID         int64      `envconfig:"GITHUB_APP_ID" required:"true"`
	GitHubAppPrivateKey []byte     `envconfig:"GITHUB_APP_PRIVATE_KEY" required:"true"`
	Mode                string     `envconfig:"MODE" default:"prod"`
	LogLevel            slog.Level `envconfig:"LOG_LEVEL" default:"info"`
}

func main() {
	if err := run(); err != nil {
		log.Printf("error: %v", err)
	}
}

func run() error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}

	ghClient, err := NewGitHubAppClient(cfg)
	if err != nil {
		return fmt.Errorf("couldn't initialize GitHub client: %w", err)
	}

	logger := newLogger(cfg)

	handle := githubevents.New(cfg.WebhookSecret)
	handle.OnPullRequestEventSynchronize(func(deliveryID, eventName string, event *github.PullRequestEvent) error {
		fullName := strings.Split(event.GetRepo().GetFullName(), "/")
		if len(fullName) != 2 {
			return fmt.Errorf("invalid repo name '%s'", event.GetRepo().GetFullName())
		}

		owner := fullName[0]
		repo := fullName[1]
		installationID := event.GetInstallation().GetID()
		logger := logger.With(
			"owner", owner,
			"repo", repo,
			"installationID", installationID,
		)
		logger.Debug("got PR sync hook")

		client, err := ghClient.GetInstallationClient(context.TODO(), installationID)
		if err != nil {
			logger.Error("couldn't get install token", "err", err)
			return fmt.Errorf("couldn't get install token for repo: %w", err)
		}

		_, _, err = client.Checks.CreateCheckRun(context.TODO(), owner, repo, github.CreateCheckRunOptions{
			Name:       "Example Release Check",
			HeadSHA:    event.GetPullRequest().GetMergeCommitSHA(),
			Status:     github.String("completed"),
			Conclusion: github.String("failure"),
		})
		if err != nil {
			logger.Error("couldn't create check", "err", err)
			return fmt.Errorf("couldn't create check: %w", err)
		}

		return nil
	})
	handle.OnIssueCommentCreated(func(deliveryID, eventName string, event *github.IssueCommentEvent) error {
		fullName := strings.Split(event.GetRepo().GetFullName(), "/")
		if len(fullName) != 2 {
			return fmt.Errorf("invalid repo name '%s'", event.GetRepo().GetFullName())
		}

		owner := fullName[0]
		repo := fullName[1]
		installationID := event.GetInstallation().GetID()
		commentID := event.GetComment().GetID()

		logger := logger.With(
			"owner", owner,
			"repo", repo,
			"installationID", installationID,
			"commentID", commentID,
		)
		logger.Debug("got comment hook")
		client, err := ghClient.GetInstallationClient(context.TODO(), installationID)
		if err != nil {
			logger.Error("couldn't get install token", "err", err)
			return fmt.Errorf("couldn't get install token for repo: %w", err)
		}

		_, _, err = client.Reactions.CreateCommentReaction(context.TODO(), owner, repo, commentID, "eyes")
		if err != nil {
			logger.Error("couldn't create reaction", "err", err)
			return fmt.Errorf("couldn't create issue reaction: %w", err)
		}
		// comment, _, err := client.Issues.GetComment(context.Background(), owner, repo, commentID)
		// if err != nil {
		// 	logger.Error("couldn't get comment", "err", err)
		// 	return fmt.Errorf("couldn't get comment: %w", err)
		// }

		logger.Info("added reaction",
			"reaction", "eyes")
		return nil
	})

	// add a http handleFunc
	http.HandleFunc("/hook", func(w http.ResponseWriter, r *http.Request) {
		err := handle.HandleEventRequest(r)
		if err != nil {
			fmt.Println("error")
		}
	})

	log.Println("starting server on port 8080")
	// start the server listening on port 8080
	if err := http.ListenAndServe(":8080", nil); err != nil {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

func newLogger(cfg Config) *slog.Logger {
	options := &slog.HandlerOptions{Level: cfg.LogLevel}
	if cfg.Mode == "local" {
		return slog.New(slog.NewTextHandler(os.Stdout, options))
	}

	return slog.New(slog.NewJSONHandler(os.Stdout, options))
}

func LoadConfig() (Config, error) {
	godotenv.Load()
	var cfg Config
	err := envconfig.Process("", &cfg)
	return cfg, err
}
