# AWS Secrets Manager

```
{{secret:id[#key][@profile]}}
```

- **Id:** a secret name or full ARN. `secret_prefix` is prepended to names
  (not ARNs): with `secret_prefix: acme/dev/`, `{{secret:db}}` reads
  `acme/dev/db`.
- **Whole secret:** without `#key`, the full `SecretString` is used. Binary
  secrets are base64-encoded; add `|base64decode` to get the raw bytes.
- **JSON keys:** `#key` selects a top-level key of a JSON secret, which is how
  the RDS-managed and console key/value secrets are stored:
  `{{secret:acme/dev/db#password}}`.
- **Names with `@`:** end the reference with `@profile` or a bare `@`; see
  [Escaping and limits](../references.md#escaping-and-limits).

## API usage

envhaul calls `GetSecretValue` once per distinct secret, however many keys
you read from it, with up to 8 calls in flight. It deliberately doesn't use
`BatchGetSecretValue`, which needs an extra IAM permission. A secret that
doesn't exist is treated as missing.

## Permissions

`secretsmanager:GetSecretValue` on the secret ARNs, plus `kms:Decrypt` for
secrets encrypted with a customer-managed key.
