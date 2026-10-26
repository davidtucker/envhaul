# References and pipes

A reference pulls a value from a source when you run `sync`, `diff`, `get`
or `exec`:

```
{{ scheme:path [#key] [@profile] [| fn[:arg]]... }}
```

| Part | Meaning |
| --- | --- |
| `scheme` | The source: `ssm` or `secret`. |
| `path` | What to read. For SSM, paths without a leading `/` are joined onto `ssm_prefix`. For Secrets Manager, `secret_prefix` is prepended unless the id is an ARN. |
| `#key` | Optional. Select one top-level key from a JSON value. String values are used as-is; numbers, booleans, objects and arrays are written as compact JSON; `null` becomes empty. |
| `@profile` | Optional. Use this AWS profile instead of the default. |
| `\|fn` | Optional. Transform the value; see [Pipes](#pipes). |

Spaces around the parts are ignored: `{{ ssm:api/KEY | trim }}` works.

## Examples

```yaml
vars:
  # SSM, relative to ssm_prefix (/acme/dev): reads /acme/dev/api/KEY
  KEY: "{{ssm:api/KEY}}"

  # SSM, absolute path
  ZONE: "{{ssm:/acme/global/route53/ZONE_ID}}"

  # SSM, a specific version or label
  PINNED: "{{ssm:api/KEY:3}}"

  # Another account via a named profile
  SHARED: "{{ssm:api/OPENAI_API_KEY@acme-shared}}"

  # Secrets Manager, whole secret string
  CERT: "{{secret:acme/dev/tls-cert}}"

  # Secrets Manager, one key of a JSON secret, from another profile
  TOKEN: "{{secret:acme/ci/github#token@acme-shared}}"

  # Several references and literal text in one value
  DATABASE_URL: "postgresql://{{secret:db#username}}:{{secret:db#password|urlencode}}@localhost:5432/app"
```

## Pipes

Pipes run left to right on the resolved value.

| Function | Effect |
| --- | --- |
| `urlencode` | Percent-encode everything except `A-Z a-z 0-9 - _ . ~` (spaces become `%20`). Use it for passwords inside URLs. |
| `base64` | Standard base64 encode. |
| `base64decode` | Standard base64 decode, padded or unpadded. Invalid input is an error. |
| `trim` | Remove leading and trailing whitespace. |
| `lower` | Lowercase. |
| `upper` | Uppercase. |
| `default:value` | Use `value` if the value at this point is empty or missing. Everything after the first `:` is the argument, so `default:http://localhost:8000` works. |

```yaml
PASSWORD: "{{secret:db#password|trim|urlencode}}"
FLAGS: "{{ssm:api/FEATURE_FLAGS|default:none}}"
REGION: "{{ssm:optional/region|trim|lower|default:us-east-1}}"
```

A reference that ends with a `default` is never reported as missing.

## Escaping and limits

- Write a literal `{{` as `\{{`.
- Braces (`{`, `}`) and `|` can't appear inside a reference, including in a
  `default:` argument.
- Secrets Manager names may contain `@`. Because envhaul splits the profile at
  the last `@`, end such references with an explicit profile, or a bare `@`
  for the default profile: `{{secret:team@corp/key@}}`.

## Deduplication

envhaul fetches each distinct value once per run, however many variables
reference it. `{{secret:db#username}}` and `{{secret:db#password}}` cost a
single `GetSecretValue` call, and the same SSM parameter used by three
components is fetched once.
