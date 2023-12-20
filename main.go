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

const (
	checkName                   = "Example Release Check"
	overrideReleaseLockActionID = "override_rel_lock"
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
	handle.SetOnCheckSuiteEventAny(func(deliveryID, eventName string, event *github.CheckSuiteEvent) error {
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

		// client, err := ghClient.GetInstallationClient(context.TODO(), installationID)
		// if err != nil {
		// 	logger.Error("couldn't get install token", "err", err)
		// 	return fmt.Errorf("couldn't get install token for repo: %w", err)
		// }

		// check, _, err := client.Checks.CreateCheckRun(context.TODO(), owner, repo, github.CreateCheckRunOptions{
		// 	Name:    checkName,
		// 	HeadSHA: event.GetCheckSuite().GetHeadSHA(),
		// 	Status:  github.String("in_progress"),
		// 	Output: &github.CheckRunOutput{
		// 		Title:   github.String("Release locked due to pending release"),
		// 		Summary: github.String("Release locked due to pending release"),
		// 	},
		// })
		// if err != nil {
		// 	logger.Error("couldn't create check", "err", err)
		// 	return fmt.Errorf("couldn't create check: %w", err)
		// }

		// time.Sleep(10 * time.Second)

		// _, _, err = client.Checks.UpdateCheckRun(context.TODO(), owner, repo, check.GetID(), github.UpdateCheckRunOptions{
		// 	Name:       checkName,
		// 	Status:     github.String("completed"),
		// 	Conclusion: github.String("failure"),
		// 	Output: &github.CheckRunOutput{
		// 		Title:   github.String("Release locked due to failed release"),
		// 		Summary: github.String("Release locked due to failed release"),
		// 	},
		// 	Actions: []*github.CheckRunAction{
		// 		{
		// 			Label:       "Override Lock",
		// 			Description: "Override the release lock",
		// 			Identifier:  overrideReleaseLockActionID,
		// 		},
		// 	},
		// })
		// if err != nil {
		// 	logger.Error("couldn't create check", "err", err)
		// 	return fmt.Errorf("couldn't create check: %w", err)
		// }

		// logger.Info("added failure status check")
		return nil
	})

	handle.OnIssueCommentCreated(func(deliveryID, eventName string, event *github.IssueCommentEvent) error {
		if !strings.HasPrefix(event.GetComment().GetBody(), "/override") {
			return nil
		}

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

		pr, _, err := client.PullRequests.Get(context.Background(), owner, repo, event.GetIssue().GetNumber())
		if err != nil {
			logger.Error("couldn't get PR", "err", err)
			return fmt.Errorf("couldn't get PR: %w", err)
		}

		checkResults, _, err := client.Checks.ListCheckRunsForRef(context.Background(), owner, repo, pr.GetHead().GetSHA(), &github.ListCheckRunsOptions{
			CheckName: github.String(checkName),
			AppID:     github.Int64(cfg.GitHubAppID),
		})
		if err != nil {
			logger.Error("couldn't get checks", "err", err)
			return fmt.Errorf("couldn't get checks: %w", err)
		}

		var checkID int64
		for _, check := range checkResults.CheckRuns {
			checkID = check.GetID()
		}

		_, _, err = client.Checks.UpdateCheckRun(context.Background(), owner, repo, checkID, github.UpdateCheckRunOptions{
			Name:       checkName,
			Status:     github.String("completed"),
			Conclusion: github.String("success"),
			Output: &github.CheckRunOutput{
				Title:   github.String("Release lock manually overridden"),
				Summary: github.String("Release lock manually overridden"),
			},
		})
		if err != nil {
			logger.Error("couldn't update check", "err", err)
			return fmt.Errorf("couldn't update checks: %w", err)
		}

		_, _, err = client.Reactions.CreateIssueCommentReaction(context.TODO(), owner, repo, commentID, "eyes")
		if err != nil {
			logger.Error("couldn't create reaction", "err", err)
			return fmt.Errorf("couldn't create issue reaction: %w", err)
		}
		logger.Debug("added reaction",
			"reaction", "eyes")

		logger.Info("override release lock")
		return nil
	})

	handle.OnCheckRunEventRequestAction(func(deliveryID, eventName string, event *github.CheckRunEvent) error {
		if event.GetRequestedAction().Identifier != overrideReleaseLockActionID {
			return nil
		}

		fullName := strings.Split(event.GetRepo().GetFullName(), "/")
		if len(fullName) != 2 {
			return fmt.Errorf("invalid repo name '%s'", event.GetRepo().GetFullName())
		}

		owner := fullName[0]
		repo := fullName[1]
		installationID := event.GetInstallation().GetID()
		checkID := event.GetCheckRun().GetID()
		prs := Map(event.GetCheckRun().PullRequests, func(pr *github.PullRequest) int {
			return pr.GetNumber()
		})

		logger := logger.With(
			"owner", owner,
			"repo", repo,
			"installationID", installationID,
			"pullRequests", prs,
		)

		client, err := ghClient.GetInstallationClient(context.TODO(), installationID)
		if err != nil {
			logger.Error("couldn't get install token", "err", err)
			return fmt.Errorf("couldn't get install token for repo: %w", err)
		}

		_, _, err = client.Checks.UpdateCheckRun(context.Background(), owner, repo, checkID, github.UpdateCheckRunOptions{
			Name:       checkName,
			Status:     github.String("completed"),
			Conclusion: github.String("success"),
			Output: &github.CheckRunOutput{
				Title:   github.String("Release lock manually overridden"),
				Summary: github.String("Release lock manually overridden"),
			},
		})
		if err != nil {
			logger.Error("couldn't update check", "err", err)
			return fmt.Errorf("couldn't update checks: %w", err)
		}

		logger.Info("override release lock")
		return nil
	})

	handle.SetOnWorkflowRunEventAny(func(deliveryID, eventName string, event *github.WorkflowRunEvent) error {
		if event.GetWorkflow().GetPath() != ".github/workflows/deploy.yaml" { // FIXME parameterize this
			return nil
		}

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

		client, err := ghClient.GetInstallationClient(context.TODO(), installationID)
		if err != nil {
			logger.Error("couldn't get install token", "err", err)
			return fmt.Errorf("couldn't get install token for repo: %w", err)
		}

		prs, _, err := client.PullRequests.List(context.Background(), owner, repo, &github.PullRequestListOptions{})
		if err != nil {
			logger.Error("couldn't get PRs", "err", err)
			return fmt.Errorf("couldn't get PRs: %w", err)
		}

		var status *string
		var conclusion *string
		var title *string
		if event.GetAction() == "completed" {
			status = github.String("completed")
			if event.GetWorkflowRun().GetConclusion() == "success" {
				conclusion = github.String("success")
				title = github.String("Release unlocked")
			} else if event.GetWorkflowRun().GetConclusion() == "failure" {
				conclusion = github.String("failure")
				title = github.String("Release locked due to failed deployment")
			}
		} else if event.GetAction() == "requested" {
			status = github.String("in_progress")
			title = github.String("Release locked due to pending deployment")
		}

		for _, pr := range prs {
			checkResults, _, err := client.Checks.ListCheckRunsForRef(context.Background(), owner, repo, pr.GetHead().GetSHA(), &github.ListCheckRunsOptions{
				CheckName: github.String(checkName),
				AppID:     github.Int64(cfg.GitHubAppID),
			})
			if err != nil {
				logger.Error("couldn't get checks", "err", err)
				return fmt.Errorf("couldn't get checks: %w", err)
			}

			checkID := checkResults.CheckRuns[0].GetID()

			_, _, err = client.Checks.UpdateCheckRun(context.Background(), owner, repo, checkID, github.UpdateCheckRunOptions{
				Name:       checkName,
				Status:     status,
				Conclusion: conclusion,
				Output: &github.CheckRunOutput{
					Title:   title,
					Summary: title,
				},
			})
			if err != nil {
				logger.Error("couldn't update check", "err", err)
				return fmt.Errorf("couldn't update checks: %w", err)
			}
		}

		return nil
	})

	// add a http handleFunc
	http.HandleFunc("/api/webhook", func(w http.ResponseWriter, r *http.Request) {
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

func Map[T, U any](tt []T, fn func(T) U) []U {
	var ret []U
	for _, t := range tt {
		ret = append(ret, fn(t))
	}

	return ret
}
