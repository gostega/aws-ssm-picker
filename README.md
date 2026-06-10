# aws-ssm-picker

An interactive terminal UI for launching [AWS Systems Manager (SSM)](https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager.html) sessions onto EC2 instances — no SSH keys, no bastion hosts, no copying instance IDs around.

Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) and [Lip Gloss](https://github.com/charmbracelet/lipgloss).

## Features

- **Profile picker** — if `AWS_PROFILE` isn't set, choose from the profiles in `~/.aws/config` (with type-to-filter), or type any profile name
- **Automatic SSO login** — if your credentials are expired, it runs `aws sso login` for you before continuing
- **Instance picker** — lists running EC2 instances in the region with name, instance ID, and type; full-text filtering as you type
- **Direct connect** — pass an instance name, instance ID, or alias as an argument to skip the picker
- **Session reason** — optionally record a reason for the session, which is attached to the `StartSession` API call and visible in CloudTrail
- **Aliases** — define shorthand names for frequently used instances

## Prerequisites

- [AWS CLI v2](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html)
- [Session Manager plugin](https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html) for the AWS CLI
- AWS profiles configured in `~/.aws/config`
- Target instances must have the SSM agent running and an instance profile permitting Session Manager

## Install

```sh
go install github.com/gostega/aws-ssm-picker@latest
```

Or build from source:

```sh
git clone https://github.com/gostega/aws-ssm-picker.git
cd aws-ssm-picker
go build -o aws-ssm-picker .
```

## Usage

```sh
# Interactive: pick a profile, then pick an instance
aws-ssm-picker

# Use a specific profile and region
AWS_PROFILE=my-profile AWS_REGION=us-east-1 aws-ssm-picker

# Connect straight to an instance by name, ID, or alias
aws-ssm-picker my-web-server
aws-ssm-picker i-0123456789abcdef0
aws-ssm-picker web
```

If `AWS_REGION` is not set, the region is resolved from the selected profile's configuration.

After choosing an instance you're prompted for an optional reason (press Enter to skip); it's recorded in CloudTrail against the session. The tool then `exec`s `aws ssm start-session`, so your terminal becomes the session directly.

## Aliases

Create `~/.aws_ssm_aliases` with one entry per line:

```sh
ALIASES[web]="my-web-server"
ALIASES[db]="i-0123456789abcdef0"
```

An alias maps a shorthand to either an instance name or an instance ID. The format is intentionally `bash`-compatible so the same file can be sourced from shell scripts.

## License

[MIT](LICENSE)
