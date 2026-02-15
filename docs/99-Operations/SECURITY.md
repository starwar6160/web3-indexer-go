# Security Policy

## 🔒 Secret Management

This project implements **multiple layers of protection** to prevent API keys, passwords, and other secrets from being committed.

### 1. Project-Level Protection (.gitignore)

The `.gitignore` file explicitly blocks all files that may contain secrets:

```gitignore
# --- CORE SECURITY: NEVER COMMIT THESE FILES ---
.env
.env.local
.env.*.local
config/secrets.yaml
*.key
*.pem
*.secret
*.credentials
*.auth
*.token
*.password
```

### 2. Global Protection (git-secrets)

We use `git-secrets` to scan file contents before commits:

#### Installed Global Patterns:
- AWS Access Keys: `AKIA[0-9A-Z]{16}`
- OpenRouter API Keys: `sk-or-v1-[a-zA-Z0-9]{48}`
- GitHub Personal Access Tokens: `ghp_[a-zA-Z0-9]{36}`
- Generic API Keys: `API_KEY`
- Database URLs: `DATABASE_URL=`
- RPC URLs: `RPC_URL`
- Secret Keys: `SECRET_KEY`
- Private Keys: `PRIVATE_KEY`
- Passwords: `PASSWORD=`
- Tokens: `TOKEN=`

#### Installation:
```bash
# Install git-secrets
git clone https://github.com/awslabs/git-secrets.git
cd git-secrets
sudo make install

# Configure global rules
git secrets --register-aws --global
git secrets --add --global 'API_KEY'
git secrets --add --global 'DATABASE_URL='
# ... add more patterns

# Install hooks in repository
git secrets --install -f
```

### 3. Environment Configuration

#### Never commit real API keys. Use placeholders:
```bash
# ❌ WRONG - Real secrets
API_KEY=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
DATABASE_URL=postgres://user:realpassword@localhost:5432/db

# ✅ CORRECT - Placeholders
API_KEY=YOUR_OPENROUTER_API_KEY
DATABASE_URL=postgres://user:password@localhost:5432/db
RPC_URLS=https://eth-mainnet.g.alchemy.com/v2/YOUR_ALCHEMY_KEY
```

#### Required Environment Variables:
```bash
# Copy .env.example to .env and fill with real values
cp .env.example .env

# Edit .env with your actual keys
nano .env
```

### 4. Pre-commit Hook Behavior

If you try to commit secrets, git-secrets will block it:

```
[ERROR] Matched one or more prohibited patterns

Possible mitigations:
- Mark false positives as allowed using: git config --add secrets.allowed ...
- Use --no-verify if this is a one-time false positive
```

### 5. Checking for Committed Secrets

To scan existing commits for secrets:
```bash
# Scan entire repository history
git secrets --scan-history

# Scan specific files
git secrets --scan $(git ls-files)
```

### 6. Emergency Procedures

If secrets are accidentally committed:

#### Option 1: Remove and Rewrite History
```bash
# Remove the file
git rm --cached filename_with_secrets
echo "filename_with_secrets" >> .gitignore

# Rewrite history (WARNING: This rewrites public history)
git filter-branch --force --index-filter \
  'git rm --cached --ignore-unmatch filename_with_secrets' \
  --prune-empty --tag-name-filter cat -- --all

# Force push
git push --force-with-lease origin main
```

#### Option 2: Rotate All Exposed Keys
1. Immediately revoke all exposed API keys
2. Generate new keys from service providers
3. Update local environment variables
4. Remove the compromised commit from history

### 7. Service-Specific Key Rotation

If any of these keys were exposed, rotate them immediately:

| Service | Rotation Link |
|---------|---------------|
| OpenRouter | https://openrouter.ai/keys |
| Alchemy | https://dashboard.alchemy.com/apps |
| Infura | https://infura.io/dashboard |
| GitHub | https://github.com/settings/tokens |
| AWS | https://console.aws.amazon.com/iam/home#/security_credentials |

### 8. Best Practices

1. **Never hard-code secrets** in source code
2. **Use environment variables** for all configuration
3. **Rotate keys regularly** (every 90 days)
4. **Use least-privilege access** for API keys
5. **Monitor usage** of all API keys
6. **Commit .env.example** with placeholders only
7. **Add .env to .gitignore** immediately

### 9. Monitoring and Alerts

Set up alerts for:
- Unusual API usage patterns
- Failed authentication attempts
- New API key generation
- Git commits that bypass hooks

### 10. Compliance

This security policy helps maintain compliance with:
- GDPR (personal data protection)
- SOC 2 (security controls)
- ISO 27001 (information security)
- Industry best practices

## 🚨 Incident Response

If a secret leak is discovered:

1. **Immediate Actions** (within 1 hour):
   - Revoke all exposed keys
   - Rotate all credentials
   - Notify security team

2. **Containment** (within 4 hours):
   - Remove secrets from repository
   - Rewrite Git history if needed
   - Scan all systems for exposure

3. **Recovery** (within 24 hours):
   - Deploy new keys
   - Update documentation
   - Review security policies

4. **Post-mortem** (within 1 week):
   - Root cause analysis
   - Process improvements
   - Security training

---

**Remember**: Security is everyone's responsibility. When in doubt, ask the security team before committing.
