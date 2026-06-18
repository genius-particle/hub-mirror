package main

import (
	"context"
	"fmt"
	"os"
	"text/template"

	"github.com/spf13/pflag"
	"github.com/togettoyou/hub-mirror/pkg"
)

var (
	content    = pflag.String("content", "", "Issue Form body（为空时回退读 env ISSUE_BODY）")
	repository = pflag.String("repository", "", "推送仓库地址，为空默认为 hub.docker.com")
	username   = pflag.String("username", "", "仓库用户名")
	password   = pflag.String("password", "", "仓库密码")
	outputPath = pflag.String("outputPath", "output.md", "结果输出路径")
)

func main() {
	pflag.Parse()

	body := *content
	if body == "" {
		body = os.Getenv("ISSUE_BODY")
	}

	fmt.Println("解析 Issue Form")
	sections := pkg.ParseIssueForm(body)

	platform := sections["Platform"]
	if platform == "" {
		platform = "linux/amd64"
	}
	image := sections["Image"]
	tag := sections["Tag"]
	dockerfile := pkg.ExtractFenced(sections["Dockerfile"])

	if image == "" || tag == "" || dockerfile == "" {
		panic(fmt.Sprintf("校验失败：image/tag/dockerfile 不能为空（image=%q, tag=%q, dockerfile 长度=%d）", image, tag, len(dockerfile)))
	}

	fmt.Printf("image: %s, tag: %s, platform: %s\n", image, tag, platform)

	fmt.Println("初始化 Docker 客户端")
	cli, err := pkg.NewCli(context.Background(), *repository, *username, *password, os.Stdout)
	if err != nil {
		panic(err)
	}

	target, err := cli.BuildTarget(image, tag)
	if err != nil {
		panic(err)
	}

	fmt.Println("开始构建镜像", target)
	if err := cli.BuildImage(context.Background(), dockerfile, platform, target); err != nil {
		panic(fmt.Errorf("构建失败: %w", err))
	}

	fmt.Println("开始推送镜像", target)
	if err := cli.PushImage(context.Background(), target, platform); err != nil {
		panic(fmt.Errorf("推送失败: %w", err))
	}

	tmpl, err := template.ParseFiles("output-build.tmpl")
	if err != nil {
		panic(err)
	}
	outputFile, err := os.Create(*outputPath)
	if err != nil {
		panic(err)
	}
	defer outputFile.Close()

	if err := tmpl.Execute(outputFile, map[string]string{
		"Target":   target,
		"Platform": platform,
	}); err != nil {
		panic(err)
	}

	fmt.Println("构建并推送完成")
}
