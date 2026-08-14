export default {
  branches: ["main"],
  tagFormat: "v${version}",
  plugins: [
    "@semantic-release/commit-analyzer",
    "@semantic-release/release-notes-generator",
    ["@semantic-release/changelog", { changelogFile: "CHANGELOG.md" }],
    ["@semantic-release/exec", {
      prepareCmd: "./scripts/package_release.sh ${nextRelease.version}",
    }],
    ["@semantic-release/github", {
      assets: [
        { path: "dist/*.tar.gz", label: "${nextRelease.name} — Unix archive" },
        { path: "dist/*.zip", label: "${nextRelease.name} — Windows archive" },
        { path: "dist/checksums.txt", label: "SHA-256 checksums" },
      ],
    }],
    ["@semantic-release/git", {
      assets: ["CHANGELOG.md"],
      message: "chore(release): ${nextRelease.version} [skip ci]\n\n${nextRelease.notes}",
    }],
  ],
};
