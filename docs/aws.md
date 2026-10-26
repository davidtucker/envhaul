# AWS profiles and permissions

envhaul uses the AWS SDK for Go v2 and your shared config files
(`~/.aws/config` and `~/.aws/credentials`), so every way of configuring a
profile works: IAM Identity Center (SSO), `role_arn` with `source_profile`,
`credential_process`, static keys, and environment variables.

## Which profile and region

For each reference:

- **Profile:** the reference's `@profile`, then `--profile`, then the config's
  `aws_profile`, then the SDK default (`AWS_PROFILE`, then `default`).
- **Region:** `--region`, then the config's `aws_region`, then the profile's
  `region` setting.

`aws_region` applies to every reference, including those with `@profile`. If
your accounts use different regions, leave `aws_region` unset and set
`region` on each profile instead.

Each profile and region pair loads its credentials once per run.

## Multiple accounts

A common setup keeps per-environment values in the environment's account and
shared third-party keys in a separate account:

```ini title="~/.aws/config"
[profile acme-dev]
sso_session = acme
sso_account_id = 111111111111
sso_role_name = Developer
region = us-east-1

[profile acme-shared]
sso_session = acme
sso_account_id = 222222222222
sso_role_name = SecretsReader
region = us-east-1

[sso-session acme]
sso_start_url = https://acme.awsapps.com/start
sso_region = us-east-1
```

```yaml title="envhaul.yaml"
aws_profile: acme-dev
components:
  api:
    env_file: .env
    vars:
      DB_PASSWORD: "{{secret:acme/dev/db#password}}"          # acme-dev
      OPENAI_API_KEY: "{{ssm:/shared/OPENAI_API_KEY@acme-shared}}" # acme-shared
```

## SSO sessions

When an SSO token has expired, envhaul says which command to run:

```
error: ssm: reading /acme/dev/api/KEY: AWS SSO session for profile "acme-dev" has expired or is missing; run `aws sso login --profile acme-dev`
```

## Required permissions

envhaul only reads. A least-privilege policy for the paths in your config:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "ReadParameters",
      "Effect": "Allow",
      "Action": "ssm:GetParameters",
      "Resource": "arn:aws:ssm:us-east-1:111111111111:parameter/acme/dev/*"
    },
    {
      "Sid": "ReadSecrets",
      "Effect": "Allow",
      "Action": "secretsmanager:GetSecretValue",
      "Resource": "arn:aws:secretsmanager:us-east-1:111111111111:secret:acme/dev/*"
    },
    {
      "Sid": "DecryptSecureStrings",
      "Effect": "Allow",
      "Action": "kms:Decrypt",
      "Resource": "arn:aws:kms:us-east-1:111111111111:key/YOUR-KEY-ID",
      "Condition": {
        "StringEquals": {
          "kms:ViaService": [
            "ssm.us-east-1.amazonaws.com",
            "secretsmanager.us-east-1.amazonaws.com"
          ]
        }
      }
    }
  ]
}
```

`kms:Decrypt` is only needed for SecureString parameters and secrets encrypted
with a customer-managed key; AWS-managed keys don't need it.

## Custom endpoints

envhaul honors the SDK's endpoint settings, such as `endpoint_url` in a
profile or `AWS_ENDPOINT_URL`, so it works with VPC endpoints and local
emulators like [moto](https://github.com/getmoto/moto) and LocalStack.
