<p align="center">
  <a href="https://yarn.social/">
    <img alt="Yarn social" src="https://git.mills.io/yarnsocial/assets/raw/branch/master/yarn.svg" width="220"/>
  </a>
</p>
<h1 align="center">Yarn - 一个以隐私为中心的去中心化、自托管的社交媒体平台</h1>

<p align="center">
  <a href="https://drone.mills.io/yarnsocial/yarn" title="Build Status">
    <img src="https://drone.mills.io/api/badges/yarnsocial/yarn/status.svg?ref=refs/heads/main">
  </a>
  <a href="https://goreportcard.com/report/git.mills.io/yarnsocial/yarn" title="Go Report Card">
    <img src="https://goreportcard.com/badge/git.mills.io/yarnsocial/yarn">
  </a>
  <a href="https://pkg.go.dev/git.mills.io/yarnsocial/yarn" title="GoDoc">
    <img src="https://pkg.go.dev/git.mills.io/yarnsocial/yarn?status.svg">
  </a>
  <a href="https://opensource.org/licenses/AGPLv3" title="License: AGPLv3">
    <img src="https://img.shields.io/badge/License-AGPLv3-blue.svg">
  </a>
  <a href="https://hub.docker.com/u/prologic/yarnd" title="Docker Pulls">
    <img src="https://img.shields.io/docker/pulls/prologic/yarnd">
  </a>
</p>

<p align="center">
  <a href="README.md">英文</a>
</p>

## 安装

### 二进制版本

请使用 [Releases](https://git.mills.io/yarnsocial/yarn/releases) 页面提供的二进制版本

### Homebrew

如果您使用 macOS 系统，我们提供了 [Homebrew](https://brew.sh) 安装包，其中包含了命令行客户端（`yarnc`）及服务端（`yarnd`）。

```console
brew tap yarnsocial/yarn https://git.mills.io/yarnsocial/homebrew-yarn.git
brew install yarn
```

运行服务端：

```console
yarnd
```

运行命令行客户端：

```console
yarnc
```

### 使用源代码构建

如果您熟悉 [Go](https://golang.org) 开发，按下面步骤构建：

1.  克隆仓库 (_重要_)

```console
git clone https://git.mills.io/yarnsocial/yarn.git
```

2.  安装依赖项 (_重要_)

Linux, macOS:

```console
make deps
```

请注意，为了使用媒体上传功能正常工作，您需要安装 ffmpeg 及相关 `-dev` 包。
请查阅您操作系统相关联的包及名字。

FreeBSD:

-   安装 `gmake`
-   安装 `pkgconf` （`pkg-config`）

```console
gmake deps
```

3.  编译

Linux, macOS:

```console
make
```

FreeBSD:

```console
gmake
```

## 用法

### 命令行客户端

1.  登录到 [Yarn.social](https://yarn.social) ：

```#!console
$ ./yarnc login
INFO[0000] Using config file: /Users/prologic/.twt.yaml
Username:
```

2.  查看您的动态

```#!console
$ ./yarnc timeline
INFO[0000] Using config file: /Users/prologic/.twt.yaml
> prologic (50 minutes ago)
Hey @rosaelefanten 👋 Nice to see you have a Twtxt feed! Saw your [Tweet](https://twitter.com/koehr_in/status/1326914925348982784?s=20) (_or at least I assume it was yours?_). Never heard of `aria2c` till now! 🤣 TIL

> dilbert (2 hours ago)
Angry Techn Writers ‣ https://dilbert.com/strip/2020-11-14
```

3.  发表 Twt (_推文_):

```#!console
$ ./yarnc post
INFO[0000] Using config file: /Users/prologic/.twt.yaml
Testing `yarn` the command-line client
INFO[0015] posting twt...
INFO[0016] post successful
```

查看 `yarnc` 帮助文档：

```#!console
$ yarnc help
This is the command-line client for Yarn.social pods running
yarnd. This tool allows a user to interact with a pod to view their timeline,
following feeds, make posts and managing their account.

Usage:
  yarnc [command]

Available Commands:
  completion  generate the autocompletion script for the specified shell
  help        Help about any command
  login       Login and authenticate to a Yarn.social pod
  post        Post a new twt to a Yarn.social pod
  stats       Parses and performs statistical analytis on a Twtxt feed given a URL or local file
  timeline    Display your timeline

Flags:
  -c, --config string   set a custom config file (default "/Users/prologic/.yarnc.yml")
  -D, --debug           Enable debug logging
  -h, --help            help for yarnc
  -T, --token string    yarnd API token to use to authenticate to endpoints (default "$YARNC_TOKEN")
  -U, --uri string      yarnd API endpoint URI to connect to (default "http://localhost:8000/api/v1/")

Use "yarnc [command] --help" for more information about a command.
```

### 使用 Docker Compose 部署

运行

```console
docker-compose up -d
```

然后访问：<http://localhost:8000/>

### Web

运行 yarnd:

```console
yarnd -R
```

**注意：** 默认情况下禁止用户注册，使用 `-R` 标记开放用户注册。

访问：<http://localhost:8000/>

您还可以配置其它选项或通过环境变量来配置。

使用下面命令查看可用选项：

```console
$ ./yarnd --help
```

环境变量名称全部使用大写字母并且使用 `_` 代替 `-`。

## 配置您的 Pod

最小配置项：

-   `-d /path/to/data`
-   `-s bitcask:///path/to/data/twtxt.db` (_可能会简化并默认使用这个_)
-   `-n <name>` pod 名称
-   `-u <url>` 提供网络访问的 URL (_公开URL_)
-   `-R` 开放用户注册
-   `-O` 开放用户配置

其它更多配置应使用环境变量来完成。

_建议_ 使用环境变量设置一个管理员账号：

-   `ADMIN_USER=username`
-   `ADMIN_EMAIL=email`

为了配置用于密码恢复和 `/support` 端点的电子邮件设置 `/abuse`，您应该设置适当的 `SMTP_` 值。

**强烈建议** 您还设置以下值来保护您的 Pod：

-   `API_SIGNING_KEY`
-   `COOKIE_SECRET`
-   `MAGICLINK_SECRET`

这些值应使用安全的随机数生成器生成，并且长度为 `64` 。您可以使用以下 shell 脚本为您的 pod 生成上述秘密值：

```console
$ cat /dev/urandom | tr -dc 'a-zA-Z0-9' | fold -w 64 | head -n 1
```

您可以使用 shell 脚本`./tools/gen-secrets.sh` 方便的生成 pod 生产环境的密钥，复制/粘贴到 `docker-compose.yml` 文件正确的位置。

**不要** 公开发布或分享这些值，**确保** 仅将它们设置为环境变量。

**注意：** [Dockerfile](/Dockerfile) 指定容器作为 `yarnd(uid=1000)` 用户运行， 确保您挂载到容器中并用作数据存储 (`-d/--data`) 路径和数据库存储路径 (`-s/--store`) 的任何卷都已正确配置为具有正确的用户/组所有权。例如：`chorn -R 1000:1000 /data`

## 生产环境部署

### Docker Swarm

您可以使用提供的 `yarn.yaml` 将 `yarnd` 部署到 [Docker Swarm](https://docs.docker.com/engine/swarm/) 集群，环境依赖 [Traefik](https://docs.traefik.io/) 作为 负载均衡器，因此您还必须在集群中正确配置和运行该负载均衡器。

```console
docker stack deploy -c yarn.yml
```

## 贡献

如果您对这个项目有兴趣，我们非常欢迎！您可以通过以下方式做出贡献：

-   [提交 Issue](https://git.mills.io/yarnsocial/yarn/issues/new) -- 任何 bug 或者新功能的建议或意见
-   提交 Pull-Request！ 欢迎提交 PR 改进项目！

请参阅 [项南指南](/CONTRIBUTING.md) 和 [开发文档](https://dev.twtxt.net) 或在 [/docs](/docs) 上查看。

## 贡献者

感谢所有为这个项目做出贡献的人，在他们自己的项目或产品中使用测试，修复错误，提高性能，甚至修复文档中的小错别字！谢谢你们的持续贡献！

您可以找到一个 [AUTHORS](/AUTHORS) 文件，其中保存了项目贡献者的列表。
如果您贡献 PR，请考虑在此处添加您的姓名。

## 相关项目

-   [Yarn.social](https://git.mills.io/yarnsocial/yarn.social) -- [Yarn.social](https://yarn.social) 着陆页
-   [Yarns](https://git.mills.io/yarnsocial/yarns) -- 托管在 [search.twtxt.net](https://search.twtxt.net) 的 [Yarn.social](https://yarn.social) 搜索引擎
-   [App](https://git.mills.io/yarnsocial/app) -- Flutter 实现的 iOS 和 Android 移动 App
-   [Feeds](https://git.mills.io/yarnsocial/feeds) -- 托管在 [feeds.twtxt.net](https://feeds.twtxt.net) 的 RSS/Atom/Twitter 到 [Twtxt](https://twtxt.readthedocs.org) 聚合服务

## 开源协议

`yarn` 基于 [AGPLv3](/LICENSE) 开源协议
