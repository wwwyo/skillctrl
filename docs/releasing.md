# Releasing skillctrl

Release only a tested commit on `main`. CI tests on Linux and macOS, checks formatting and vet, cross-builds macOS/Linux amd64/arm64 archives, and runs the host archive's version and CI help commands.

1. Merge the reviewed implementation PR and confirm CI passes on `main`.
2. Tag that commit with a stable semantic version and push the tag:

   ```sh
   # Set RELEASE_TAG to the version being published (vMAJOR.MINOR.PATCH).
   git tag "$RELEASE_TAG"
   git push origin "$RELEASE_TAG"
   ```

3. Wait for the **Release** workflow. It repeats validation, requires the tagged commit to belong to `main`, and publishes only after all archives have been uploaded to a draft release. Failed publication leaves the draft for inspection; reruns do not overwrite an existing release.
4. Download the generated `skillctrl.rb` asset from that release into `Formula/skillctrl.rb` in `wwwyo/homebrew-tap`. Verify its URLs and hashes against `checksums.txt`, then publish the tap change using your own authenticated session. Edit the generator in this repository for formula changes, never the generated formula. No cross-repository token is stored in this repository.
5. Verify the published version through the documented mise, Homebrew tap, and `go install` paths in temporary locations. Check `--version`, `--help`, and `ci --help`. Preserve the existing deployed CLI while verifying.

Archives contain the root `skillctrl` executable and MIT license. Go builds use the pinned mise toolchain, disabled CGO, trimmed paths, and an explicit version. Archive ordering, modes, owner IDs, and timestamps are fixed. Rebuilds with the same source, compiler, and dependencies produce the same bytes.

To build without publishing, use an empty output directory:

```sh
mise exec -- go run ./tools/release "$RELEASE_TAG" dist
```

The [mise GitHub backend](https://mise.jdx.dev/dev-tools/backends/github.html) selects uploaded assets for the host platform. A [Homebrew tap](https://docs.brew.sh/How-to-Create-and-Maintain-a-Tap) distributes the checksummed formula; this project does not claim inclusion in Homebrew core.
