# AWS SSM Parameter Store

```
{{ssm:path[#key][@profile]}}
```

- **Path:** without a leading `/`, the path is joined onto `ssm_prefix`
  (`api/KEY` with `ssm_prefix: /acme/dev` reads `/acme/dev/api/KEY`). With a
  leading `/` it's absolute. Parameters outside any hierarchy (no `/`) work
  when `ssm_prefix` is unset.
- **Versions and labels:** append `:version` or `:label`, as in
  `{{ssm:api/KEY:3}}` or `{{ssm:api/KEY:prod}}`.
- **Types:** `String`, `StringList` (written as the comma-separated value) and
  `SecureString` (always decrypted).
- **JSON:** `#key` selects a top-level key from a parameter that holds a JSON
  object.

## API usage

envhaul calls `GetParameters` with `WithDecryption=true`, batching up to 10
names per call, grouped by profile and region, with up to 8 calls in flight.
Parameters that don't exist come back from AWS as invalid parameters; envhaul
treats them as missing (see [Missing values](../config.md#missing-values)).

## Permissions

`ssm:GetParameters` on the parameter ARNs, plus `kms:Decrypt` for
SecureStrings encrypted with a customer-managed key. See
[Required permissions](../aws.md#required-permissions).
