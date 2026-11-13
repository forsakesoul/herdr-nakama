# Nakama

English | [简体中文](README.zh-CN.md) | [日本語](README.ja.md)

Turn several coding agents in [Herdr](https://herdr.dev) into a team with clear roles: one Owner designs and accepts the work, 1–4 Workers implement it, and 0–4 optional Advisors review it independently. Nakama is a native Herdr plugin that creates the team, initializes it, adds or removes members mid-task, and brings the same team back after a computer or Herdr restart.

https://github.com/user-attachments/assets/861b71da-e0f5-4123-bc7f-5a1b3bc27bec

▶ 109-second usage video · [中文版](https://github.com/user-attachments/assets/f2b21f2f-da61-4832-9113-158efeb8f4ec) · [Project page](https://forsakesoul.github.io/herdr-nakama/)

## Nakama at a glance

![herdr-nakama overview: one Owner, 1–4 Workers and 0–4 optional Advisors work in defined roles; Nakama creates the team with the counts you choose, initializes it, shows its status, rebinds it after a restart, and adds, removes or replaces members mid-task.](docs/media/nakama-overview-en.png)

Each member runs in its own pane within the same Herdr tab; role responsibilities are defined by prompts and a shared collaboration protocol, and each Worker edits only its own non-overlapping scope. See below for operating instructions and recovery conditions.

## Why "Nakama"

*Nakama* (仲間) is the word Monkey D. Luffy uses for his crew in *One Piece*: not staff or tools, but companions who each own a part of the voyage and trust the others with theirs. That is the shape of the team this plugin builds:

- **Owner — the captain.** Sets the course: investigates, freezes the handoff, accepts the result. Never writes project code.
- **Worker — the crew at work.** Only Workers change code, and they report back when done, blocked or running out of context.
- **Advisor — the navigator.** Optional; checks the plan and reviews the diff independently.

Each nakama sails in its own pane with its own engine, and the shared protocol is how they talk: frozen handoffs, NOTICEs, nobody answering for someone else. When a storm — a computer or Herdr restart — scatters the crew, `nakama rebind` finds every member again, like a Vivre Card pointing home: the same team, not a new one.

The name is a tribute, not an affiliation: this project is not associated with *One Piece*, Eiichiro Oda, Shueisha or Toei Animation.

## Install

Requires macOS and Herdr 0.9.3 or later. The install command below builds the plugin on your machine and needs Go 1.24 or later; without Go, use the release package that follows.

```sh
# 1. Install (runs the build declared in the manifest). For local development use: go build … && herdr plugin link "$PWD" --enabled
herdr plugin install forsakesoul/herdr-nakama
# 2. Private config: create from config.example.json only if missing (0600), then fill in pi_provider / cc_env_file as needed (private, never commit)
cfg="$(herdr plugin config-dir forsakesoul.nakama)/config.json"
[ -e "$cfg" ] || (umask 077 && mkdir -p "${cfg%/*}" && curl -fsSL https://raw.githubusercontent.com/forsakesoul/herdr-nakama/main/config.example.json -o "$cfg.tmp" && mv "$cfg.tmp" "$cfg")
# 3. Bind keys (append to ~/.config/herdr/config.toml), then reload
herdr server reload-config
```

### Without Go: use a release package

The release archive already contains the compiled binary, so you need neither Go nor a clone:

```sh
arch="$(uname -m | sed 's/x86_64/amd64/')"
dir="$HOME/.local/share/herdr-nakama"
mkdir -p "$dir" && curl -fsSL "https://github.com/forsakesoul/herdr-nakama/releases/latest/download/nakama-darwin-$arch.tar.gz" | tar -xz -C "$dir" --strip-components=1
herdr plugin link "$dir" --enabled
```

To pin a version, replace `latest/download` with `download/v0.0.3`. Re-run the same block to upgrade. Packages are provided from v0.0.3 onward.

Key binding example (`config.toml`):

```toml
[[keys.command]]
key = "prefix+u"
type = "plugin_action"
command = "forsakesoul.nakama.pick"
description = "nakama: pick engines and create a team anchored at the current pane"

[[keys.command]]
key = "prefix+i"
type = "plugin_action"
command = "forsakesoul.nakama.init"
description = "nakama: init (initialize the team of the current anchor)"
```

## Quick start

1. **Create a team.** In a pane inside your project directory, press `prefix+u` and choose the engines for the Owner, Workers and Advisors, plus how many Workers and Advisors you want (Enter takes the default; the Advisor can be `none`). After you confirm, the plugin splits one pane per member in the current tab and starts the agents.
2. **Initialize.** Once the agents are up (clear any approval dialog first), press `prefix+i`. Each member receives its role description and the collaboration rules.
3. **Start working.** Describe the task in the Owner's pane. The Owner splits it up and hands it to the Workers, the Workers report back when done, and the Advisors review on request.

To see team status: `herdr plugin action invoke status --plugin forsakesoul.nakama`.

## Changing members mid-task

Press `prefix+u` again in any pane of the team to open its management menu:

| Key | Action |
|---|---|
| `n` / `a` | Add a Worker / an Advisor |
| `r` / `x` | Remove a Worker / an Advisor |
| `w` / `v` | Replace a Worker / an Advisor (to change its engine, or to swap out a member that is stuck) |
| `c` | Clean up task records |

- After adding or replacing, wait for the new member to start, then press `prefix+i` once more: the new member is initialized and the others receive the updated member list.
- New members only open in the current tab. A team keeps at least 1 Worker, has at most 4 Workers and 4 Advisors, and its Owner cannot be replaced.
- The pane of a removed or replaced member is left open for you to close. Before replacing a Worker, make sure the old one has stopped writing, so two Workers never edit the same scope at once.
- When a role has more than one member, removing or replacing first asks for the member key (for example `W2`).

## Restoring after a restart

After a computer or Herdr restart every pane gets a new terminal ID, so the team has to be rebound before you can use it again:

```sh
nakama rebind --team <id> --preflight      # read-only: lists what changed
nakama rebind --team <id> --confirm-anchor <new anchor terminal>
```

Add the confirmation arguments the preflight asks for. You get the original team back, and each member keeps its conversation. For the refusal conditions and what to do after a partial failure, see the [reference](docs/reference.md).

## Task records

Each team has a `.nakama/<team id>/` directory inside the project that holds handoffs, reports and review conclusions; it is kept out of Git by default. Nothing is deleted when a task finishes: the Owner cleans up only after you say so, or you can do it yourself with `c` in the management menu or `nakama clean`.

## Configuration

Config directory: `$HERDR_PLUGIN_CONFIG_DIR` (injected by Herdr), otherwise `${XDG_CONFIG_HOME:-$HOME/.config}/nakama`.
File: `config.json`. **Never commit real providers / paths / credentials.**

```json
{
  "default_profile": {
    "owner": "sol",
    "worker": "ds",
    "advisor": "none",
    "engine_map": { "sol": "codex/<model>:high", "ds": { "owner": "pi/<model>:low", "worker": "pi/<model>:low", "advisor": "pi/<model>:high" } },
    "cc_env_file": ""
  },
  "projects": {
    "<repo-name>": { "prefix": "<short>", "context_file": "/abs/project-context.md", "trigger": ";xx.ctx" }
  }
}
```

- An engine is written as `<launcher>/<model>[:effort]`, or as a shorthand you define in `engine_map`.
- `projects` gives a repository its team-name prefix and a project context file that every member receives at initialization.
- The language of the interface and prompts is set by `NAKAMA_LANG=en|zh|ja` and otherwise follows the system locale; set it in the environment that starts Herdr.
- Launcher details (the `pi` provider, `ccenv`, the Claude model preset) are in the [reference](docs/reference.md).

## Commands

```sh
nakama up [--owner E] [--worker E] [--workers N] [--advisor E|none] [--advisors N] [--pane P] [--cwd C]
nakama init --team <id>
nakama status [--team <id>]
nakama rebind --team <id> [--preflight | --discard-pending] [--confirm-session <member-key|anchor>=<session>]... [--confirm-anchor <terminal>]
nakama add --team <id> [--role worker|advisor] [--engine E]
nakama drop --team <id> [--role worker|advisor] [--instance N]
nakama swap --team <id> --role worker|advisor [--instance N] [--engine E]
nakama clean [--team <id>] [--task <task_id>]... [--all] [--yes] [--force]
```

## Upgrading

Teams created before an upgrade keep working as conversations. If the new version changes the shared protocol, initializing those teams or adding and replacing their members is refused; create a new team instead.

## More

- [Reference](docs/reference.md): full configuration, command details, state and safety, every rule for restoring after a restart, protocol source, tests and known limitations.
- [Shared protocol and role prompts](prompts/en/): what each role actually receives.
- [Project page](https://forsakesoul.github.io/herdr-nakama/)

## Acknowledgements

- [Herdr](https://herdr.dev) ([herdrdev/herdr](https://github.com/herdrdev/herdr), Apache-2.0) — the terminal multiplexer for coding agents that Nakama plugs into; every pane, agent and rename goes through its CLI.
- [motion-use](https://github.com/leeguooooo/motion-use) (MIT) — used to direct and render the usage video, including its subtitle, text and narration checks.
- [Azure AI Speech](https://azure.microsoft.com/products/ai-services/ai-speech) — neural text-to-speech for the video narration.

## License

[MIT](LICENSE)
