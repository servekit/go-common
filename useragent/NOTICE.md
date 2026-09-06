# NOTICE — useragent

## Engine

The matching engine in this package (`useragent.go`, `rules.go`,
`data.go`) is original code written for the servekit monorepo. It is not a
port of any third-party Go implementation.

## Rule data

The regular-expression rule sets embedded under `data/` are **curated and
adapted** from the [Matomo device-detector](https://github.com/matomo-org/device-detector)
rule files (`regexes/oss.yml`, `regexes/client/browsers.yml`,
`regexes/client/mobile_apps.yml`, `regexes/client/libraries.yml`,
`regexes/device/mobiles.yml`, `regexes/bots.yml`), which are copyrighted by
the Matomo team and licensed under the **LGPL-3.0-or-later**
(https://www.gnu.org/licenses/lgpl.html).

What was taken and what was changed:

- Taken: matching ideas and many individual regex fragments, OS/browser
  names, and the overall ordering discipline (specific rules before generic
  ones, apps before browsers).
- Changed: patterns were rewritten to Go RE2 syntax (upstream relies on
  PCRE lookbehind/lookahead, unsupported by `regexp`), rules were trimmed to
  the subset our traffic needs, several rules were rewritten from scratch
  (e.g. the generic Android model fallback, the HarmonyOS/OpenHarmony
  ordering, `grpc-go`, ArkWeb and Quark ordering, the brand-prefix device
  rules). Upstream files were edited, not copied wholesale.

The LGPL-3.0-or-later license therefore applies to the embedded rule data
in `data/*.yml` as derivative works.

## Licensing implications

- **Internal monorepo use (current situation): OK.** LGPL restricts
  distribution, not internal use. Serving traffic from an internal binary
  does not trigger LGPL obligations beyond keeping this attribution.
- **If a binary embedding this package is ever distributed externally**
  (shipped to customers, published as a downloadable agent/appliance
  image), the LGPL-3.0 obligations attach to the embedded rule data: keep
  this NOTICE and the LGPL license text available to recipients, and allow
  recipients to relink/replace the LGPL-covered data. If that is
  unacceptable, either obtain a commercial license from Matomo
  (https://shop.matomo.org/) or replace `data/*.yml` with independently
  written rules before distribution.
- The Go implementation studied during design,
  [slipros/devicedetector](https://github.com/slipros/devicedetector), is
  MIT-licensed; no code was copied from it.

## Refreshing the data

When extending a category, diff against the corresponding upstream file
(see links above), copy only the rules needed for observed traffic,
rewrite PCRE-only constructs (lookbehind `(?<!...)`, lookahead `(?!...)`,
backreferences, possessive quantifiers) into RE2 equivalents, and keep the
ordering discipline documented at the top of each file. Rule files remain
derivative works of Matomo's data; do not remove this notice.
