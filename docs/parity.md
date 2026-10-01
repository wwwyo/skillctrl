# Source behavior to Go test parity

This table maps each behavior of the previous implementation to the Go code and
the test that pins it. A row marked as a gap names what is not covered and why;
no row is listed because it is unimportant.

The fixtures are ports, not transcriptions: they run the real binary against
throwaway Git repositories and fake upstreams reached through Git's
`url.insteadOf`, and they assert what is observable from outside the process
(printed JSON, exit codes, files on disk, Git state).

## Installer

| Behavior | Go code | Test |
| --- | --- | --- |
| Dry run reports intent and writes nothing | `internal/cli.runInstall` | `TestInstallerLifecycle/dry_run_writes_nothing` |
| Unchanged original is not re-imported, so an adaptation is never discarded | `internal/install.Import` fingerprint comparison | `TestInstallerLifecycle/unchanged_original_is_not_re-imported` |
| A changed original is imported and re-adapted to the saved intent | `internal/upstream.Install`, `internal/install.Adapt` | `TestInstallerLifecycle/changed_original_imports_bytes_and_modes` |
| Binary content and executable modes survive an import | `internal/upstream.Export` | `TestInstallerLifecycle/changed_original_imports_bytes_and_modes` |
| Unrelated upstream commits are ignored | `internal/upstream.Export` original-hash comparison | `TestInstallerLifecycle/unrelated_upstream_change_is_ignored` |
| Unknown skill in a selection is rejected atomically | `internal/upstream.Install` | `TestInstallerLifecycle/rejected_imports_are_atomic/unknown_skill` |
| Upstream symlink is refused | `internal/upstream.Export` mode and kind checks | `TestInstallerLifecycle/rejected_imports_are_atomic/symlink`, `TestRejectedUpstreamContent/symlink` |
| Upstream `.gitignore` is refused | `internal/upstream.Export` | `TestInstallerLifecycle/rejected_imports_are_atomic/gitignore`, `TestRejectedUpstreamContent/gitignore` |
| Upstream `.gitattributes` is refused | `internal/upstream.Export` | `TestInstallerLifecycle/rejected_imports_are_atomic/gitattributes`, `TestRejectedUpstreamContent/gitattributes` |
| Content the destination ignores is refused | `internal/upstream.rejectIgnored` | `TestInstallerLifecycle/rejected_imports_are_atomic/destination_ignored_file`, `TestRejectedUpstreamContent/destination_ignores_a_file` |
| A handwritten skill is never overwritten | `internal/upstream.Install` | `TestInstallerLifecycle/rejected_imports_are_atomic/handwritten_skill` |
| A source carrying a credential is refused | `internal/upstream.Source` | `TestInstallerLifecycle/rejected_imports_are_atomic/credential_in_source`, `TestSourceRejectsAnythingButAGitHubIdentifier` |
| `update` on an unregistered skill is refused | `internal/upstream.Install` | `TestInstallerLifecycle/rejected_imports_are_atomic/unregistered_update` |
| A skill name that escapes the directory is refused | `internal/install.Names` | `TestInstallerLifecycle/rejected_imports_are_atomic/traversal_name`, `TestNamesRejectsEscapes` |
| A rejected import leaves no partial change | `internal/cli.runInstall` staging order | `TestInstallerLifecycle/rejected_imports_are_atomic` |
| Unresolved adaptation exits 2 and keeps the old hash | `internal/install.Adapt` | `TestInstallerLifecycle/unresolved_adaptation_exits_2_and_keeps_the_old_hash` |
| Failed adaptation exits 1 and keeps the old hash | `internal/install.Adapt` | `TestInstallerLifecycle/failed_adaptation_exits_1_and_keeps_the_old_hash` |
| A reviewer that edits outside the selection is refused | `internal/adapt.ReviewLocal`, `internal/adapt.ValidatePaths` | `TestInstallerLifecycle/reviewer_scope_violation_is_refused` |
| The reviewer runs isolated, and its output is exported | `internal/adapt.ReviewLocal` | `TestInstallerLifecycle/reviewer_is_isolated_and_its_output_is_exported` |
| A skill without an intent is recorded without invoking the reviewer | `internal/install.Adapt` | `TestInstallerLifecycle/intent-free_import_needs_no_reviewer` |
| `remove` deletes the registration but keeps the intent file | `internal/install.Import` | `TestInstallerLifecycle/remove_keeps_the_intent_file` |
| The relative Claude link is created and maintained | `internal/install.Import` | `TestInstallerLifecycle/intent-free_import_needs_no_reviewer` |
| `record` accepts a manual edit without a worktree or staging change | `internal/cli.runRecord` | `TestInstallerLifecycle/record_preserves_staging_in_the_main_checkout`, `TestWorkingTreePreserveStaging` |

## Accepted hashes and selection

| Behavior | Go code | Test |
| --- | --- | --- |
| A hash covers the whole skill directory, including executable mode | `internal/lock.Snapshot` | `TestExecutableBitIsCovered` |
| An accepted hash stops review | `internal/lock.Select` | `TestHashesDetectUnrecordedEdits` |
| An intent change alone never triggers adaptation | `internal/lock.Select` | `TestHashesDetectUnrecordedEdits` |
| An intent deletion alone never triggers adaptation | `internal/lock.Select` | `TestHashesDetectUnrecordedEdits` |
| A removed skill with a surviving intent is reported, not restored | `internal/lock.Compare` | `TestHashesDetectUnrecordedEdits`, `TestAcceptedRequiresACompletePartition/removed_skill...` |
| Recording a removed skill clears its entry | `internal/lock.Record` | `TestRecordPreservesUnrelatedEntries` |
| The working tree is hashed through a private index | `internal/lock.WorkingTree` | `TestWorkingTreePreserveStaging` |
| An unsupported lock version is refused | `internal/lock.Parse` | `TestParseRejectsUnsupportedLocks` |

## Upstream lock and search

| Behavior | Go code | Test |
| --- | --- | --- |
| A relocated original is followed by name | `internal/upstream.Select` | `TestRelocatedOriginalFollowsTheSkill` |
| A declared name and a directory name resolve to one skill | `internal/upstream.indexSkills` | `TestRelocatedOriginalFollowsTheSkill` |
| An ambiguous name is refused | `internal/upstream.Select` | `TestAmbiguousSkillIsRefused` |
| A repository whose root is the skill imports as a whole tree | `internal/upstream.Export` | `TestRepositoryRootSkillIsTheWholeDirectory` |
| Unknown lock fields survive an update | `internal/upstream.Record` | `TestLoadPreservesUnknownFields`, `TestRelocatedOriginalFollowsTheSkill` |
| An unsupported upstream lock is refused | `internal/upstream.Load` | `TestLoadRejectsUnsupportedLocks` |
| Search results are bounded and unusable entries are skipped | `internal/upstream.Find` | `TestFindBoundsAndSkips` |
| Invalid search input never reaches the network | `internal/upstream.Find` | `TestFindBoundsAndSkips` |

## Intent verification and CI

| Behavior | Go code | Test |
| --- | --- | --- |
| Only the trusted tool pins and release policy are used | `internal/toolchain.Trusted`, `internal/adapt.Prepare` | `TestPrepareUsesOnlyTrustedPins` |
| A version range is refused as a pin | `internal/toolchain.NodeVersion` | `TestToolchainRequiresExactPins` |
| An incomplete trusted configuration is refused | `internal/toolchain.Trusted` | `TestToolchainRefusesAnIncompleteConfiguration` |
| A moved head is refused | `internal/adapt.ValidateHead` | `TestPrepareRefusesHeadDrift`, `TestApplyRecordsFromTheIndexOnly` |
| The accepted hash comes from the validated index, not the working tree | `internal/adapt.Apply` | `TestApplyRecordsFromTheIndexOnly` |
| Edits to an intent, another skill, or a policy file are refused | `internal/adapt.ValidatePaths` | `TestApplyRefusesEditsOutsideTheSelection` |
| The accepted/unresolved partition must be complete and exact | `internal/adapt.Accepted` | `TestAcceptedRequiresACompletePartition` |
| An unresolved skill must be unchanged | `internal/adapt.Accepted` | `TestAcceptedRequiresACompletePartition/unresolved_skill...` |
| A credential is refused in the body, report, result, or a staged blob | `internal/adapt.ValidateSecret` | `TestWriteRepairArtifactRefusesCredentials` |
| A missing credential is itself a refusal | `internal/adapt.ValidateSecret` | `TestWriteRepairArtifactRefusesCredentials/a_missing_credential...` |
| The report is bounded before it reaches GitHub | `internal/adapt.ReadReport` | `TestReadReportBounds` |
| A closed, moved, forked, or self-targeted pull request is refused | `internal/adapt.Publish` | `TestPublishRefusesAnythingButTheCurrentPullRequest` |
| A rerun does not post a duplicate comment | `internal/adapt.Publish` | `TestPublishReportsOnce` |
| Restored merge content never becomes a repair input | `internal/adapt.Export`, `internal/adapt.ReviewLocal` | `TestEngineHandoffExportsOnlySelectedSkills` |
| The reviewer runs with an isolated agent configuration | `internal/adapt.ReviewLocal` | `TestEngineHandoffExportsOnlySelectedSkills`, `TestReviewLocalUsesOnlyTheTrustedToolchain` |

## Isolation of the reviewing agent

These fixtures keep three directories apart - the caller's, the checkout under
review, and the prepared trusted configuration - so a regression cannot pass by
accident.

| Behavior | Go code | Test |
| --- | --- | --- |
| The toolchain is resolved in the prepared directory, never in the caller's | `internal/adapt.toolchainEnvironment` | `TestReviewLocalUsesOnlyTheTrustedToolchain` |
| The reviewer runs in the checkout under review | `internal/adapt.ReviewLocal` | `TestReviewLocalUsesOnlyTheTrustedToolchain` |
| Trusted values replace inherited ones | `internal/adapt.toolchainEnvironment` | `TestReviewLocalUsesOnlyTheTrustedToolchain` |
| The reviewer executable is resolved from the trusted PATH | `internal/adapt.resolveCommand` | `TestReviewerIsNotInheritedFromTheCallerPath` |
| A credential supplied by the trusted toolchain is screened | `internal/adapt.ReviewLocal` | `TestCredentialFromTheTrustedEnvironmentIsScreened` |
| An untrusted configuration is never evaluated | `internal/adapt.toolchainEnvironment` | `TestReviewLocalUsesOnlyTheTrustedToolchain` |
| No review runs at all without a trusted toolchain | `internal/adapt.ReviewLocal` | `TestReviewLocalRequiresATrustedToolchain` |

## Scheduled updates

| Behavior | Go code | Test |
| --- | --- | --- |
| An unchanged original produces no work and no bundle | `internal/scheduled.Prepare` | `TestPrepareAndRestoreProduceAVerifiedInput/an_unchanged...` |
| A changed original is validated before it becomes input | `internal/scheduled.ValidateImport` | `TestPrepareAndRestoreProduceAVerifiedInput/a_changed...` |
| A forged plan does not restore | `internal/scheduled.Restore` | `TestPrepareAndRestoreProduceAVerifiedInput/the_input_restores...` |
| An unregistered skill change is refused | `internal/scheduled.ValidateImport` | `TestValidateImportRefusesAnythingButOriginals/an_unregistered...` |
| A changed upstream identity is refused | `internal/scheduled.ValidateImport` | `TestValidateImportRefusesAnythingButOriginals/a_changed_registration` |
| A broken Claude link is refused | `internal/scheduled.ValidateImport` | `TestValidateImportRefusesAnythingButOriginals/a_broken_Claude_link` |
| An added or removed registration is refused | `internal/scheduled.ValidateImport` | `TestValidateImportRefusesAnythingButOriginals/an_added_registration` |
| A failing repository test stops publication | `internal/scheduled.Publish` | `TestPublishRunsVerificationBeforeOpeningAPullRequest/a_failing...` |
| A moved default branch stops publication | `internal/scheduled.Publish` | `TestPublishRunsVerificationBeforeOpeningAPullRequest/a_moved...` |
| A verified update opens one draft pull request | `internal/scheduled.Publish` | `TestPublishRunsVerificationBeforeOpeningAPullRequest/a_verified...` |
| An unresolved skill keeps the update unresolved | `internal/scheduled.Publish` | `TestPublishRunsVerificationBeforeOpeningAPullRequest/an_unresolved...` |
| An existing update is reused instead of duplicated | `internal/scheduled.OpenUpdate` | `TestPublishRunsVerificationBeforeOpeningAPullRequest/an_unresolved...` |

## Isolation of the install target

| Behavior | Go code | Test |
| --- | --- | --- |
| The installer never mutates the caller's checkout | `internal/install.Worktree` | `TestWorktreeIsolationCreatesACleanCheckout` |
| A dirty main checkout refuses to start | `internal/install.Worktree` | `TestWorktreeRefusesADirtyMainCheckout` |
| An existing linked worktree is reused | `internal/install.Worktree` | `TestWorktreeReusesAnExistingIsolationBoundary` |
| An unknown worktree provider is refused | `internal/install.Worktree` | `TestWorktreeRejectsAnUnknownProvider` |
| A symlink inside a skill is refused | `internal/install.CheckSkills` | `TestCheckSkillsRefusesSymlinks` |

## Command surface

| Behavior | Go code | Test |
| --- | --- | --- |
| Every command is discoverable in help | `internal/cli.New` | `TestHelpIsDiscoverable` |
| The bare invocation shows help | `internal/cli.New` | `TestBareInvocationShowsHelp` |
| An argument or flag error exits non-zero with an explanation and a clean stdout | `internal/cli.Execute` | `TestArgumentErrorsAreReported` |
| `schema` describes the commands, options, and exit codes | `internal/cli.newSchemaCommand` | `TestSchemaDescribesTheContract` |
| A module installation reports its version without linker flags | `internal/cli.BuildVersion` | `TestVersionReflectsTheInstalledModule`, `TestInjectedVersionWins` |
| A failure is one JSON object on stderr | `internal/cli.fail` | `TestFailuresAreJSONOnStderr` |

## Gaps

- **Live reviewer.** The reviewer is a stub in every test. Nothing here
  demonstrates that a real model produces a patch that passes the same
  validation; the contract is verified on both sides of the boundary only.
- **Live `skills.sh` response.** Search parsing runs against a fixed local
  response. The real endpoint's current schema is not pinned by a test.
- **Live `mise`.** The trusted toolchain resolution is exercised through a stub
  that reports its working directory. The real `mise env --json` output shape is
  trusted, not verified here.
- **Orca worktree provider.** `--worktree-provider orca` is implemented and
  documented but only the `git` provider is tested; the Orca path was not
  exercised against a live Orca instance in this port.
- **`gh-aw` engine.** The sandbox handoff is driven by a harness in
  `internal/adapt/testdata/engine.cjs` that mirrors the gh-aw engine's
  arguments and steps. A platform-specific sandbox may supply slightly
  different steps; only the contract this tool defines is tested.
