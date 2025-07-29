# Test Data for External Secrets SOPS

This directory contains test data for the external-secrets-sops project, organized in a hierarchical structure that mimics real-world usage patterns.

## Directory Structure

```
testdata/
├── src/                    # Source files (unencrypted)
│   ├── project1/
│   │   └── config1/
│   │       ├── base/
│   │       ├── dev/
│   │       └── prod/
│   ├── project2/
│   │   ├── config1/
│   │   └── config2/
│   └── project3/
│       └── config1/
├── generated/              # Encrypted files (auto-generated)
├── decrypted/              # Decrypted files (for verification)
└── scripts/
    ├── encrypt.sh          # Encrypt source files
    └── decrypt.sh          # Decrypt generated files
```

## Test Data Patterns

### Naming Conventions
- Files ending with `_UNENCRYPTED` are not encrypted by SOPS
- All other values are encrypted
- Supports both `.yaml` and `.yml` extensions

### Projects and Configurations
- **project1/config1**: Basic web application secrets
- **project2/config1**: OAuth and API service secrets  
- **project2/config2**: Database and infrastructure secrets
- **project3/config1**: Payment processing secrets

### Environments
- **base**: Common configuration shared across environments
- **dev**: Development environment specific secrets
- **staging**: Staging environment configuration
- **prod**: Production environment secrets

## Usage

### Encrypt all source files:
```bash
./encrypt.sh
```

### Decrypt all generated files (for verification):
```bash
./decrypt.sh
```

### Test API endpoints:
```bash
# Get all secrets for a specific configuration
curl http://localhost:8080/secret/project1/config1/dev

# Get a specific secret
curl http://localhost:8080/secret/project1/config1/dev/DATABASE_PASSWORD
```

## Secret Types Included

- Database credentials (PostgreSQL, MySQL, MongoDB)
- API keys and tokens
- JWT secrets
- SSL certificates and keys
- OAuth credentials
- Third-party service tokens (Stripe, PayPal, Datadog, Sentry)
- Infrastructure secrets (Redis, RabbitMQ, Elasticsearch)
- Cloud provider credentials (AWS S3)
