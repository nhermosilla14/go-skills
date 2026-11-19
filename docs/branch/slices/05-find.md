# Find skills

- Status: Done

- Intents: 20261119-find-skills
- ADRs: 20261119-skills-sh-search

## Scope

`find` (`f`, `s`, `search`): keyword and interactive search over the skills.sh endpoint, `--owner`,
defensive parsing, 10-second timeout, no credentials, results printed with install commands, exit
codes. Plus the one-time interactive find-skills offer after a successful install, recorded in the
global lock's `dismissed` object.

## End-to-end test

`TestE2EFindKeyword`, `TestE2EFindOwner` against an `httptest` server injected through an
environment override; `TestE2EFindNoResults`, `TestE2EFindAPIFailure`, and
`TestE2EFindSkillsOfferOnce` for the dismissal flag.

## Out of scope

- Offline cache and pagination beyond the documented cap.
