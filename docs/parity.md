# Source behavior to Go test parity

This table maps current supported behaviors to Go code and observable tests.
Acquisition and accepted hashes retain their safety contracts; external
automation replaces the previous internal CI phase protocol.

The fixtures are ports, not transcriptions: they run the real binary against
throwaway Git repositories and fake upstreams reached through Git's
`url.insteadOf`, and they assert what is observable from outside the process
(printed JSON, exit codes, files on disk, Git state).

## Installer

| Behavior | Go code | Test |
| --- | --- | --- |
| Dry run reports intent and writes nothing | `internal/cli.runInstall` | `TestInstallerLifecycle/dry_run_writes_nothing` |
| Unchanged original is not re-imported, so an adaptation is never discarded | `internal/install.Import` fingerprint comparison | `TestInstallerLifecycle/unchanged_original_is_not_re-imported` |
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
| Repeated names collapse however they are ordered | `internal/install.Names` | `TestNamesRejectsEscapes` |
| `remove` deletes the skill, its intent, and registrations | `internal/install.Import` | `TestInstallerLifecycle/remove_deletes_the_selected_intent` |
| The relative Claude link is created and maintained | `internal/install.Import` | `TestInstallerLifecycle/intent-free_import_needs_no_reviewer` |
| `record` accepts a manual edit without a worktree or staging change | `internal/cli.runRecord` | `TestInstallerLifecycle/record_preserves_staging_in_the_main_checkout`, `TestWorkingTreePreserveStaging` |

## Multiple upstream inputs

These behaviors extend the previous single-source implementation.

| Behavior | Go code | Test |
| --- | --- | --- |
| Unchanged originals preserve output; intent-only edits do not trigger integration | `internal/upstream.mergeSkill`, `internal/lock.Select` | `TestMergeTracksEverySourceAndUpdatePreservesUnchangedOutput` |
| Dry run and failure in a later input do not write partial results; routing requires no saved intent | `internal/cli.runInstall`, `internal/upstream.mergeSkill` | `TestMergeDryRunAndPreparationFailureDoNotWrite` |
| An ignored merged entrypoint is refused; the first merge in a main checkout uses the current empty skills directory | `internal/upstream.mergeSkill`, `internal/install.PrepareSkills` | `TestMergeDryRunAndPreparationFailureDoNotWrite`, `TestFirstMergeUsesTheMainCheckout` |
| Malformed or duplicate source registrations are refused; legacy records are preserved | `internal/upstream.Load`, `internal/upstream.sources` | `TestSourcesArrayRejectsInvalidIdentitiesAndPreservesLegacyRecords` |
| Inputs from separate repositories follow relocation and retain executable modes; snapshot manifests are not independently discovered | `internal/upstream.mergeSkill`, `internal/upstream.indexSkills` | `TestMergedSourcesFromDifferentRepositoriesFollowRelocations` |

## Accepted hashes and selection

| Behavior | Go code | Test |
| --- | --- | --- |
| A hash covers the whole skill directory, including executable mode | `internal/lock.Snapshot` | `TestExecutableBitIsCovered` |
| Matching an accepted hash clears content drift | `internal/lock.Select` | `TestHashesDetectUnrecordedEdits` |
| Only upstream-registered skills with intent enter accepted hashes and drift selection; handwritten intents do not opt in | `internal/lock.Select`, `internal/cli.runRecord` | `TestUpstreamOperationsIgnoreHandwrittenSkills`, `TestHandwrittenRepositoryNeedsNoLockOrReview` |
| Legacy handwritten hashes are pruned without reviewing or changing their content | `internal/lock.Select`, `internal/lock.Record` | `TestSelectionIgnoresHandwrittenSkillsAndPrunesLegacyHashes`, `TestRecordPrunesLegacyHandwrittenHashesWithoutReview` |
| An intent change alone never triggers adaptation | `internal/lock.Select` | `TestHashesDetectUnrecordedEdits` |
| An intent deletion alone never triggers adaptation | `internal/lock.Select` | `TestHashesDetectUnrecordedEdits` |
| A removed skill with a surviving intent is reported, not restored | `internal/lock.Select` | `TestHashesDetectUnrecordedEdits` |
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

## Automation uses basic commands

The former CI and scheduled phase protocols are intentionally removed following
the user's simplification decision. Their adaptation, publication, checker
toolchain, and bundle fixtures are not part of the current contract. An external
workflow uses `update`, direct review/editing, `record NAME`, and `check`; a fixed
JSON predicate makes lock drift fail CI. See [agent workflow guide](../.agents/skills/skillctrl/references/ci.md).

## Git plumbing

| Behavior | Go code | Test |
| --- | --- | --- |
| A foreign `GIT_DIR`, `GIT_WORK_TREE`, or `GIT_INDEX_FILE` cannot redirect the selected repository or touch the caller's index | `internal/gitx.Environment` | `internal/gitx/gitx_test.go` |
| A deliberately private index still works | `internal/lock.WorkingTree` | `TestWorkingTreePreserveStaging` |

## Isolation of the install target

| Behavior | Go code | Test |
| --- | --- | --- |
| Only imports replacing skill directories with pending edits are refused, before any skill is imported | `internal/install.Import` | `TestInstallerRefusesOnlyReplacementsThatLosePendingSkillEdits` |
| A symlink inside a skill is refused | `internal/install.CheckSkills` | `TestCheckSkillsRefusesSymlinks` |

## Command surface

| Behavior | Go code | Test |
| --- | --- | --- |
| Every command is discoverable in help | `internal/cli.New` | `TestHelpIsDiscoverable` |
| Check reports selected local accepted-hash drift offline without accepting or reviewing | `internal/cli.newCheckCommand`, `internal/install.Selection` | `TestCheckReportsSelectedLocalDriftWithoutAcceptingIt`, `TestCheckIgnoresUpstreamChangesAndUnavailableAdapters` |
| The bare invocation shows help | `internal/cli.New` | `TestBareInvocationShowsHelp` |
| An argument or flag error exits non-zero with an explanation and a clean stdout | `internal/cli.Execute` | `TestArgumentErrorsAreReported` |
| A module installation reports its version without linker flags | `internal/cli.BuildVersion` | `TestVersionReflectsTheInstalledModule`, `TestInjectedVersionWins` |
| A failure is one JSON object on stderr | `internal/cli.fail` | `TestFailuresAreJSONOnStderr` |

## Gaps

Acquisition fixtures use controlled sources. Public backend acquisition has also
been exercised separately, but not every host, discovery convention, release,
or locale is covered. Repository-specific agent review, hosted timers, scope
lint, and PR publication are external integrations and are not verified by the
CLI tests. A matching accepted hash does not prove intent satisfaction.

## Project lock compatibility

| Behavior | Go boundary | Evidence |
| --- | --- | --- |
| Native version-1 registrations stay at the root, preserve other providers and unknown metadata, and skip unchanged originals | `internal/upstream.Load`, `internal/upstream.Install` | `TestNativeProjectLockIsKeptAtTheRepositoryRoot` |
| Legacy repository-local registrations migrate only after successful import; reads and dry runs never migrate | `internal/upstream.Load`, `internal/install.Import` | `TestLegacyProjectLockMigratesOnlyAfterSuccessfulImport` |
| An existing root lock wins over the legacy lock; malformed root data is not replaced | `internal/upstream.Load`, `internal/upstream.Read` | `TestExistingRootLockTakesPrecedenceOverTheLegacyLock` |
| Source content hashing uses the project lock's path-and-content convention | `internal/upstream.contentHash` | `TestProjectContentHashMatchesSkillsCLIOrdering`; golden generated with Node's `localeCompare` and `crypto` |

The native project lock convention was checked against
[`src/local-lock.ts`](https://github.com/vercel-labs/skills/blob/main/src/local-lock.ts).
The hash fixture covers punctuation, case, accented and Japanese file names. It
is not a proof that every host's locale and ICU version produce identical hashes.

## Acquisition adapters and shared commands

| Behavior | Observable coverage |
| --- | --- |
| Both command adapters acquire in disposable staging/home, redact the inference credential, preserve downloaded CRLF bytes despite global Git attributes, and preserve caller staging and unrelated edits | `TestCommandAdaptersPreserveProjectState` |
| Native project registration and an unchanged local adaptation survive update/check | `TestCommandAdaptersPreserveProjectState` |
| Local check ignores upstream changes and succeeds with unavailable upstreams or failing acquisition adapters | `TestCheckIgnoresUpstreamChangesAndUnavailableAdapters` |
| Failed or unknown adapters do not silently fall back or mutate project files | `TestAdapterErrorsDoNotFallBack` |
| Common add/install, find/search, list/ls and remove/rm names plus grouped help | `TestHelpIsDiscoverable`, `TestCommandAdaptersPreserveProjectState` |

Manual acquisition against the public `wwwyo/skillctrl` source succeeded with
`skills` 1.7.0 and `gh` 2.101.0 for add/list and upstream acquisition.
Those earlier checks preceded the offline-only check contract; current CLI
fixtures prove local checks require neither installer nor upstream access. GitHub CLI search was also exercised. These runs do not prove every
native discovery convention, release/ref form, or operating-system combination.
External model adaptation is separate from acquisition. Local commands do not
launch a model or create a worktree.

Native source tracking with a missing path is refused before export (`TestCommandAdapterRejectsMissingTrackingPath`).

## Acquisition and explicit intent operations

| Behavior | Observable coverage |
| --- | --- |
| Pure merge generates links to complete originals without intent, AI, or acceptance; re-merge refreshes ordering | `TestPureMergeProducesRoutingWithoutIntent` |

| Named reference behavior | Observable coverage |
| --- | --- |
| Re-merge retains handwritten references, removes obsolete originals, and rejects colliding names or unregistered reference overlap | `TestNamedMergePreservesUnregisteredReferencesAndRejectsCollisions` |
| Legacy numeric layouts update in place; explicit merge migrates sources and routing | `TestLegacyMergeUpdatesInPlaceAndExplicitMergeMigratesReferences` |

## Shared upstream input syntax

| Behavior | Observable coverage |
| --- | --- |
| Add and merge share positional owner/repo:skill inputs; add preserves separate source identities, original bytes, local naming, and caller staging without review or acceptance | `TestAddInputsTracksSeparateSourcesAndNames` |
| Invalid, duplicate, colliding, or multiply named inputs fail before acquisition, including dry runs | `TestAddInputsRejectsInvalidInputsWithoutAcquisition`, `TestArgumentErrorsAreReported` |
| Failed acquisition or overlapping pending edits prevents every input from being imported | `TestAddInputsPreparesEveryInputBeforeImporting` |

## Direct editing and local mutation

| Behavior | Observable coverage |
| --- | --- |
| Add, merge, changed update, remove, and record use the selected main checkout, preserve staging/unrelated edits, and invoke no reviewer or worktree manager | `TestLocalCommandsUseTheMainCheckoutWithoutWorktreeOrReviewerDependencies` |
| First merge uses the selected main checkout's empty skills directory | `TestFirstMergeUsesTheMainCheckout` |
| Direct intent editing changes eligibility; deleting intent prunes acceptance on the next record without removing the skill/upstream | `TestNamedAddAndIntentLifecycle` |
| Name-free record prunes ineligible hashes without accepting eligible edits, even after every intent is removed; skill content and staging remain unchanged | `TestRecordWithoutNamesOnlyPrunesIneligibleHashes` |
| Removed intent commands and worktree-provider flags are rejected | `TestArgumentErrorsAreReported` |

## Native lock compatibility

| Behavior | Implementation | Evidence |
| --- | --- | --- |
| Keep alias/merge registrations outside native root lock; preserve its bytes and caller staging | `internal/upstream.tracking`, `internal/install.Import` | `TestNamedAndMergedRegistrationsLeaveNativeLockUntouched` |
| Emit native ordinary registrations; invalidate supplemental metadata after native edits/removal | `internal/upstream.combine`, `nativeEntry` | `TestOrdinaryNativeRegistrationWinsOverSupplementalMetadata` |

| Legacy recognized provenance moves to private tracking only after successful import; future unknown fields remain native | `internal/upstream.nativeEntry` | `TestLegacyProvenanceMovesOutOfNativeLockOnlyOnImport` |
