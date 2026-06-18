package pkg

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildTarget_WithRepository(t *testing.T) {
	cli := &Cli{username: "togettoyou", repository: "registry.cn-hangzhou.aliyuncs.com/hubmirrorbytogettoyou"}

	got, err := cli.BuildTarget("my-app", "v1.0")
	assert.Nil(t, err)
	assert.Equal(t, "registry.cn-hangzhou.aliyuncs.com/hubmirrorbytogettoyou/my-app:v1.0", got)
}

func TestBuildTarget_DefaultDockerHub(t *testing.T) {
	cli := &Cli{username: "togettoyou", repository: ""}

	got, err := cli.BuildTarget("my-app", "latest")
	assert.Nil(t, err)
	assert.Equal(t, "docker.io/togettoyou/my-app:latest", got)
}

func TestBuildTarget_EmptyImageOrTag(t *testing.T) {
	cli := &Cli{username: "togettoyou", repository: "reg.example.com/ns"}

	_, err := cli.BuildTarget("", "v1")
	assert.NotNil(t, err)

	_, err = cli.BuildTarget("my-app", "")
	assert.NotNil(t, err)
}
