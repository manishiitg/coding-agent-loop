## 📋 Pull Request Security Checklist

Before submitting this PR, please ensure:

### 🔒 Security Checks
- [ ] No hardcoded secrets or API keys in the code
- [ ] All sensitive data uses environment variables
- [ ] No `.env` files or sensitive config files are included
- [ ] No passwords, tokens, or credentials in the code
- [ ] No database connection strings with credentials
- [ ] No AWS keys, GitHub tokens, or other service credentials

### 🧪 Testing
- [ ] Pre-commit hooks are installed and working
- [ ] Manual secret scan completed: `./scripts/scan-secrets.sh`
- [ ] All tests pass
- [ ] Code follows security best practices

### 📝 Documentation
- [ ] Security-related changes are documented
- [ ] Environment variables are documented
- [ ] Configuration changes are explained

## 🔍 Security Scan Results

<!-- The GitHub Actions will automatically run security scans, but you can also run locally: -->

```bash
# Run local security scan
./scripts/scan-secrets.sh

# Install pre-commit hooks (if not already done)
./scripts/install-git-hooks.sh
```

## 📋 Description

<!-- Provide a clear description of your changes -->

## 🔗 Related Issues

<!-- Link to any related issues -->

## 🧪 Testing

<!-- Describe how you tested your changes -->

## 📸 Screenshots

<!-- If applicable, add screenshots -->

---

**Security Note**: This PR will be automatically scanned for secrets. If any are detected, the PR will be blocked until they are removed.
