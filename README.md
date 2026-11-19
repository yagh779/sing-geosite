# sing-geosite

**English** | [简体中文](README.zh.md)

A minimal fork of `sing-geosite` that builds exactly two sing-box rule-sets from
the trimmed `dlc.dat` published by
[`yagh779/domain-list-community`](https://github.com/yagh779/domain-list-community):

| Tag | Description |
| --- | --- |
| `cn@!cn` | Domains in the `cn` list that carry the `!cn` attribute (services originating outside mainland China with no mainland access point). |
| `geolocation-!cn@cn` | Domains in the `geolocation-!cn` list that carry the `cn` attribute. |

For each tag both a sing-box **source** rule-set (`.json`, `version: 3`) and a
**compiled binary** rule-set (`.srs`) are generated under `rule-set/`.

## Build

```sh
go run .
# Force a rebuild even when the upstream tag data is unchanged
NO_SKIP=true go run .
```

Output:

```
rule-set/cn@!cn.json
rule-set/cn@!cn.srs
rule-set/geolocation-!cn@cn.json
rule-set/geolocation-!cn@cn.srs
```

## Change detection

Before building, the program computes a stable fingerprint of the upstream
`cn@!cn` and `geolocation-!cn@cn` entries and compares them with
`sync-state.json`:

```json
{
  "cn@!cn": "<sha256>",
  "geolocation-!cn@cn": "<sha256>"
}
```

When both fingerprints are unchanged, the build and publish steps are skipped.
After a successful build the two fingerprints are written back to
`sync-state.json` and committed to `main`.

## Publication

The generated files are published to the `rule-set` branch. Only the `main` and
`rule-set` branches are used.

## Data source

- Source: [`yagh779/domain-list-community`](https://github.com/yagh779/domain-list-community) (`dlc.dat` from the `release` branch)
- Publish target: [`yagh779/sing-geosite`](https://github.com/yagh779/sing-geosite) (`rule-set` branch)

The source repository is itself a repackaging fork of
[`v2fly/domain-list-community`](https://github.com/v2fly/domain-list-community),
with a small `@!cn` overlay sourced from
[`nekolsd/domain-list-community`](https://github.com/nekolsd/domain-list-community).
