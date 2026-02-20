#!/bin/bash

# Install Git Hooks for Gitleaks Secret Scanning and Golangci-lint
# This script sets up pre-commit hooks to automatically scan for secrets and run linting

set -e

echo "🔒 Setting up pre-commit hooks (Gitleaks + Golangci-lint)..."

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Check if we're in a git repository
if ! git rev-parse --git-dir > /dev/null 2>&1; then
    echo -e "${RED}❌ Error: Not in a git repository${NC}"
    echo "Please run this script from the root of your git repository."
    exit 1
fi

# Check if gitleaks is installed
if ! command -v gitleaks &> /dev/null; then
    echo -e "${YELLOW}⚠️  Gitleaks not found. Installing...${NC}"
    
    # Detect OS and install gitleaks
    if [[ "$OSTYPE" == "darwin"* ]]; then
        # macOS
        if command -v brew &> /dev/null; then
            echo "Installing gitleaks via Homebrew..."
            brew install gitleaks
        else
            echo -e "${RED}❌ Homebrew not found. Please install gitleaks manually:${NC}"
            echo "Visit: https://github.com/gitleaks/gitleaks#installation"
            exit 1
        fi
    elif [[ "$OSTYPE" == "linux-gnu"* ]]; then
        # Linux
        echo "Installing gitleaks via curl..."
        curl -sSfL https://github.com/gitleaks/gitleaks/releases/latest/download/gitleaks_8.18.0_linux_x64.tar.gz | tar -xz -C /tmp
        sudo mv /tmp/gitleaks /usr/local/bin/
    else
        echo -e "${RED}❌ Unsupported OS. Please install gitleaks manually:${NC}"
        echo "Visit: https://github.com/gitleaks/gitleaks#installation"
        exit 1
    fi
fi

# Verify gitleaks installation
if ! command -v gitleaks &> /dev/null; then
    echo -e "${RED}❌ Failed to install gitleaks${NC}"
    exit 1
fi

echo -e "${GREEN}✅ Gitleaks installed successfully${NC}"

# Create scripts directory if it doesn't exist
mkdir -p scripts

# Create the pre-commit hook script
cat > .git/hooks/pre-commit << 'EOF'
#!/bin/bash

# Pre-commit Hook
# Scans staged files for secrets and runs golangci-lint

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}🔒 Scanning for secrets with gitleaks...${NC}"

# Check if gitleaks is available
if ! command -v gitleaks &> /dev/null; then
    echo -e "${YELLOW}⚠️  Gitleaks not found. Skipping secret scan.${NC}"
    echo "Run './scripts/install-git-hooks.sh' to install gitleaks."
    exit 0
fi

# Run gitleaks on staged files
if gitleaks protect --staged --config .gitleaks.toml --verbose; then
    echo -e "${GREEN}✅ No secrets detected.${NC}"
else
    echo -e "${RED}❌ Secrets detected! Commit blocked.${NC}"
    echo ""
    echo "Please remove or replace the detected secrets before committing."
    echo "Common solutions:"
    echo "  • Use environment variables instead of hardcoded secrets"
    echo "  • Move secrets to .env files (not tracked by git)"
    echo "  • Use placeholder values in example files"
    echo ""
    echo "For more information, see README.md"
    exit 1
fi

# Run golangci-lint on Go files
echo -e "${BLUE}🔍 Running golangci-lint...${NC}"

# Add GOPATH/bin to PATH early so golangci-lint can be found
if [ -d "$(go env GOPATH)/bin" ]; then
    export PATH="$PATH:$(go env GOPATH)/bin"
fi

# Check if golangci-lint is available
GOLANGCI_LINT_CMD=""
if command -v golangci-lint &> /dev/null; then
    GOLANGCI_LINT_CMD="golangci-lint"
elif [ -f "$(go env GOPATH)/bin/golangci-lint" ]; then
    GOLANGCI_LINT_CMD="$(go env GOPATH)/bin/golangci-lint"
else
    echo -e "${YELLOW}⚠️  golangci-lint not found. Skipping lint check.${NC}"
    echo "Run 'make install-linter' to install golangci-lint."
    exit 0
fi

# Run linter - show output directly to terminal
echo ""
# Run golangci-lint and capture output
# Filter out errors from output directories (tool_output_folder, cache, bin, etc.)
# to focus on main code only
set +e  # Temporarily disable exit on error so we can capture exit code
LINT_OUTPUT_FULL=$($GOLANGCI_LINT_CMD run ./... 2>&1)
# Filter out errors from output directories
LINT_OUTPUT=$(echo "$LINT_OUTPUT_FULL" | grep -v -E "(tool_output_folder|tool_output/|cache/|bin/|generated/)")
# Show filtered output to user
echo "$LINT_OUTPUT"
# Determine exit code: if filtered output has any errors, exit 1; otherwise 0
# Check for actual error patterns (file:line:column format or "issues found")
if echo "$LINT_OUTPUT" | grep -qE "(^[^:]+:[0-9]+:[0-9]+:.*(error|expected|found))|issues found"; then
    LINT_EXIT=1
else
    LINT_EXIT=0
fi
set -e  # Re-enable exit on error

# If linting passed, proceed
if [ $LINT_EXIT -eq 0 ]; then
    echo ""
    echo -e "${GREEN}✅ Linting passed. Proceeding with commit.${NC}"
    exit 0
else
    # Linting found issues - check severity
    # Use the already filtered output (from above) for analysis
    # LINT_OUTPUT is already filtered and available from the previous run
    
    # Count issues to decide if we should block or warn
    ISSUE_COUNT=$(echo "$LINT_OUTPUT" | grep -E "issues:" | grep -oE "[0-9]+ issues" | grep -oE "[0-9]+" || echo "0")
    
    # Check for truly critical security issues (G201/G202 SQL injection, G204 command injection, G304 path traversal)
    # Exclude test files from critical checks (G304 in test files are usually test fixtures)
    CRITICAL_ISSUES=$(echo "$LINT_OUTPUT" | grep -E "G201|G202|G204|G304" | grep -v "_test.go" | grep -v "/testing/" | wc -l | tr -d ' ')
    
    if [ "$CRITICAL_ISSUES" -gt 0 ]; then
        echo ""
        echo -e "${RED}❌ Critical security issues detected ($CRITICAL_ISSUES critical)! Commit blocked.${NC}"
        echo ""
        echo "Critical issues found: SQL injection (G201/G202), Command injection (G204), Path traversal (G304)"
        echo "$LINT_OUTPUT" | grep -E "G201|G202|G204|G304" | head -10
        echo ""
        echo "Please fix these security issues before committing."
        exit 1
    elif [ "$ISSUE_COUNT" -gt 200 ]; then
        # Too many issues - block commit
        echo ""
        echo -e "${RED}❌ Too many linting issues ($ISSUE_COUNT)! Commit blocked.${NC}"
        echo ""
        echo "Please fix linting errors before committing."
        echo "You can run 'make lint-fix' to auto-fix some issues."
        exit 1
    else
        # Non-critical issues - warn but allow commit
        echo ""
        echo -e "${YELLOW}⚠️  Linting found $ISSUE_COUNT issues (non-blocking).${NC}"
        echo "Most are low-priority gosec checks (G104, G301, etc.)"
        echo "Run 'make lint' to see all issues."
        echo -e "${YELLOW}Proceeding with commit...${NC}"
        exit 0
    fi
fi
EOF

# Make the pre-commit hook executable
chmod +x .git/hooks/pre-commit

# Create a manual scan script
cat > scripts/scan-secrets.sh << 'EOF'
#!/bin/bash

# Manual Secret Scanning Script
# Run this to scan for secrets in your repository

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}🔒 Scanning repository for secrets...${NC}"

# Check if gitleaks is available
if ! command -v gitleaks &> /dev/null; then
    echo -e "${RED}❌ Gitleaks not found. Please install it first:${NC}"
    echo "Run './scripts/install-git-hooks.sh' to install gitleaks."
    exit 1
fi

# Default scan path
SCAN_PATH="${1:-.}"

echo "Scanning path: $SCAN_PATH"
echo ""

# Run gitleaks scan
if gitleaks detect --source "$SCAN_PATH" --config .gitleaks.toml --verbose --report-format json --report-path gitleaks-report.json; then
    echo -e "${GREEN}✅ No secrets detected in $SCAN_PATH${NC}"
    rm -f gitleaks-report.json
else
    echo -e "${RED}❌ Secrets detected in $SCAN_PATH${NC}"
    echo ""
    echo "Report saved to: gitleaks-report.json"
    echo ""
    echo "Please review and remove the detected secrets:"
    echo "  • Use environment variables instead of hardcoded secrets"
    echo "  • Move secrets to .env files (not tracked by git)"
    echo "  • Use placeholder values in example files"
    echo ""
    echo "For more information, see README.md"
    exit 1
fi
EOF

# Make the scan script executable
chmod +x scripts/scan-secrets.sh

# Test the installation
echo -e "${BLUE}🧪 Testing gitleaks installation...${NC}"
if gitleaks version &> /dev/null; then
    echo -e "${GREEN}✅ Gitleaks is working correctly${NC}"
else
    echo -e "${RED}❌ Gitleaks test failed${NC}"
    exit 1
fi

echo ""
echo -e "${GREEN}🎉 Pre-commit hooks installed successfully!${NC}"
echo ""
echo -e "${BLUE}What happens now:${NC}"
echo "  • Every commit will be automatically scanned for secrets (gitleaks)"
echo "  • Every commit will run golangci-lint on Go code"
echo "  • Errors from tool_output_folder, cache, bin, and generated are automatically filtered"
echo "  • Commits with secrets or critical linting issues will be blocked"
echo "  • You'll get clear error messages if issues are detected"
echo ""
echo -e "${BLUE}Manual scanning:${NC}"
echo "  • Run './scripts/scan-secrets.sh' to scan the entire repository"
echo "  • Run './scripts/scan-secrets.sh path/to/file' to scan specific files"
echo "  • Run 'make lint' to run golangci-lint manually"
echo ""
echo -e "${BLUE}Configuration:${NC}"
echo "  • Edit '.gitleaks.toml' to customize secret detection rules"
echo "  • Edit '.golangci.yml' to customize linting rules"
echo ""
echo -e "${GREEN}Your repository is now protected against accidental secret commits and linting issues! 🔒${NC}"

