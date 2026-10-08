# Make the unified skillctrl skill the only entrypoint

- Status: Accepted
- Date: 2026-10-03

Made the unified `skillctrl` skill — which combines the search and creation
workflows — the only entrypoint, deleting the two older skills and switching
their references in the same change
([dotfiles #248](https://github.com/wwwyo/dotfiles/pull/248)). Keeping
`find-skills` and `skill-creator` alongside it would have made the skills'
trigger conditions overlap.
