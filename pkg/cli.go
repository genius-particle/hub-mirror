package pkg

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/client"
)

type Cli struct {
	cli        *client.Client
	repository string
	username   string
	auth       string
	log        io.Writer
}

func NewCli(ctx context.Context, repository, username, password string, log io.Writer) (*Cli, error) {
	if username == "" || password == "" {
		return nil, errors.New("username or password cannot be empty")
	}

	authConfig := registry.AuthConfig{
		Username:      username,
		Password:      password,
		ServerAddress: repository,
	}
	encodedJSON, err := json.Marshal(authConfig)
	if err != nil {
		return nil, err
	}

	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, err
	}
	_, err = cli.RegistryLogin(ctx, authConfig)
	if err != nil {
		return nil, err
	}

	return &Cli{
		cli:        cli,
		repository: repository,
		username:   username,
		auth:       base64.URLEncoding.EncodeToString(encodedJSON),
		log:        log,
	}, nil
}

type Output struct {
	Source string
	Target string
}

func (c *Cli) Source2Target(source string, platform string) (*Output, error) {
	if source == "" {
		return nil, errors.New("source is nil")
	}

	target := source

	if strings.Contains(source, "$") {
		parts := strings.Split(source, "$")
		source = parts[0]
		target = parts[1]
	}

	if !strings.Contains(target, ":") && strings.Contains(source, ":") {
		parts := strings.Split(source, ":")
		target += ":" + parts[1]
	}

	if platform != "" {
		if strings.Contains(target, ":") {
			parts := strings.Split(target, ":")
			target = parts[0] + "-" + strings.ReplaceAll(platform, "/", "-") + ":" + parts[1]
		} else {
			target += "-" + strings.ReplaceAll(platform, "/", "-")
		}
	}

	if c.repository == "" {
		target = "docker.io/" + c.username + "/" + strings.ReplaceAll(target, "/", ".")
	} else {
		target = c.repository + "/" + strings.ReplaceAll(target, "/", ".")
	}

	return &Output{
		Source: source,
		Target: target,
	}, nil
}

func (c *Cli) PullTagPushImage(ctx context.Context, source, platform string) (*Output, error) {
	output, err := c.Source2Target(source, platform)
	if err != nil {
		return nil, err
	}

	err = c.PullImage(ctx, output.Source, platform)
	if err != nil {
		return nil, err
	}

	err = c.cli.ImageTag(ctx, output.Source, output.Target)
	if err != nil {
		return nil, err
	}

	err = c.PushImage(ctx, output.Target, platform)
	if err != nil {
		return nil, err
	}

	return output, nil
}

type streamMessage struct {
	Stream       string `json:"stream,omitempty"`
	Error        string `json:"error,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`
	ErrorDetail  *struct {
		Message string `json:"message,omitempty"`
	} `json:"errorDetail,omitempty"`
}

// decodeStream reads the docker daemon JSON stream line by line, writes each
// line to the log (if set), and returns the first error reported by the daemon.
// It accepts all three error shapes used across pull/push/build:
// {"error":"..."}, {"errorMessage":"..."}, {"errorDetail":{"message":"..."}}.
func (c *Cli) decodeStream(rd io.Reader) error {
	reader := bufio.NewReader(rd)
	var msg streamMessage
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		if c.log != nil {
			_, _ = c.log.Write(line)
		}

		msg = streamMessage{}
		_ = json.Unmarshal(line, &msg)
		if msg.Error != "" {
			return errors.New(msg.Error)
		}
		if msg.ErrorMessage != "" {
			return errors.New(msg.ErrorMessage)
		}
		if msg.ErrorDetail != nil && msg.ErrorDetail.Message != "" {
			return errors.New(msg.ErrorDetail.Message)
		}
	}
}

func (c *Cli) PullImage(ctx context.Context, image, platform string) error {
	pullOut, err := c.cli.ImagePull(ctx, image, types.ImagePullOptions{Platform: platform})
	defer func() {
		if pullOut != nil {
			pullOut.Close()
		}
	}()
	if err != nil {
		return err
	}
	return c.decodeStream(pullOut)
}

func (c *Cli) PushImage(ctx context.Context, image, platform string) error {
	pushOut, err := c.cli.ImagePush(ctx, image, types.ImagePushOptions{
		RegistryAuth: c.auth,
		Platform:     platform,
	})
	defer func() {
		if pushOut != nil {
			pushOut.Close()
		}
	}()
	if err != nil {
		return err
	}
	return c.decodeStream(pushOut)
}
