#!/usr/bin/env bash
# 用法：tools/package_release.sh <outdir>（在仓库根运行）
# 为 darwin/arm64 与 darwin/amd64 各生成一个可直接 `herdr plugin link` 的目录压缩包，并写出 SHA256SUMS。
set -euo pipefail

outdir="${1:?usage: tools/package_release.sh <outdir>}"
mkdir -p "$outdir"
outdir="$(cd "$outdir" && pwd)"

stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT

for arch in arm64 amd64; do
  root="$stage/$arch/nakama"
  mkdir -p "$root/bin"
  CGO_ENABLED=0 GOOS=darwin GOARCH="$arch" \
    go build -trimpath -ldflags "-s -w" -o "$root/bin/nakama" ./cmd/nakama
  cp -R herdr-plugin.toml prompts config.example.json LICENSE \
    README.md README.zh-CN.md README.ja.md "$root/"
  tar -czf "$outdir/nakama-darwin-$arch.tar.gz" -C "$stage/$arch" nakama
done

cd "$outdir"
shasum -a 256 nakama-darwin-arm64.tar.gz nakama-darwin-amd64.tar.gz > SHA256SUMS
