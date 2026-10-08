# Apply the English default to code comments and public reviews

- Status: Accepted
- Date: 2026-10-02

Extended the English-default policy to code comments and public-facing bot
reviews. In [PR #4](https://github.com/wwwyo/skillctrl/pull/4) the repository
configuration stopped inheriting the organization's Japanese review output,
and an actual English review was verified rather than trusting the language
of the configuration file.
