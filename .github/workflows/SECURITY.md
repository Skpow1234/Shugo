# GitHub Actions Security Configuration

This document explains the security permissions and best practices implemented in the Shugo GitHub Actions workflows.

## Security Permissions

All workflows in this repository follow the **principle of least privilege** by explicitly setting minimal required permissions.

### Global Permissions

Each workflow file includes a `permissions` block with the following minimal permissions:

```yaml
permissions:
  contents: read          # Read repository contents
  pull-requests: read     # Read pull request information
  issues: read           # Read issue information
  checks: write          # Write check results
  actions: read          # Read workflow information
  security-events: write # Write security events (for security workflows)
```

### Permission Breakdown

| Permission | Purpose | Required For |
|------------|---------|--------------|
| `contents: read` | Read source code and files | All workflows need to checkout code |
| `pull-requests: read` | Read PR information | PR-based workflows |
| `issues: read` | Read issue information | Issue-related workflows |
| `checks: write` | Write check results | All workflows need to report status |
| `actions: read` | Read workflow information | Workflow execution |
| `security-events: write` | Write security alerts | Security scanning workflows |

## Workflow-Specific Permissions

### CI/CD Pipeline (`ci.yml`)

- **Purpose**: Tiered build/test gate — required jobs (lint, unit+race, P2P e2e, REST smoke, govulncheck, multi-OS build, Docker) plus soft warn-only suites
- **Permissions**: `contents: read`, `checks: write`, `security-events: write`
- **Security**: No write access to repository contents; merge on **CI Success** only
- **Concurrency**: Cancels superseded runs on the same PR/branch

### Security Pipeline (`security.yml`)

- **Purpose**: Comprehensive security scanning
- **Permissions**: Standard permissions + security-events write
- **Security**: Can write security alerts but not modify code

### Dependabot Security (`dependabot-security.yml`)

- **Purpose**: Dependency vulnerability scanning
- **Permissions**: Standard permissions + security-events write
- **Security**: Read-only access to dependencies

### Development Security (`security-dev.yml`)

- **Purpose**: Quick security validation for PRs
- **Permissions**: Standard permissions + security-events write
- **Security**: Limited to security-related file changes

## Security Benefits

### 1. **Principle of Least Privilege**

- Workflows only have the minimum permissions required
- Reduces attack surface if workflow is compromised
- Prevents accidental or malicious code modifications

### 2. **Explicit Permission Declaration**

- Clear documentation of what each workflow can do
- Easy to audit and review permissions
- Prevents permission creep over time

### 3. **Separation of Concerns**

- Different workflows have different permission levels
- Security workflows can write security events but not code
- Build workflows can write artifacts but not modify source

## CodeQL Integration

[`codeql.yml`](codeql.yml) is the advanced CodeQL setup. Go analysis uses a manual `go build ./...` step (default setup’s autobuild left Go unprocessed on this repo). JS/TS and Actions use `build-mode: none`.

Use **advanced** setup only: Settings → Advanced Security → CodeQL → Switch to advanced / disable default setup after this workflow is on `main`.

Workflow permissions address CodeQL rule `actions/missing-workflow-permissions` by:

1. **Explicit Permission Declaration**: All workflows explicitly declare permissions
2. **Minimal Required Permissions**: Only necessary permissions are granted
3. **Security-First Approach**: Security workflows have appropriate security permissions

## Best Practices Implemented

### 1. **No Write Access to Contents**

- Workflows cannot modify source code
- Prevents malicious code injection
- Maintains repository integrity

### 2. **Limited Pull Request Access**

- Read-only access to PR information
- Cannot modify PR content or metadata
- Can only read for context and validation

### 3. **Controlled Security Events**

- Security workflows can write security alerts
- Enables proper security monitoring
- Does not grant general write access

### 4. **Explicit Permission Blocks**

- Every workflow has a permissions block
- No inheritance of overly broad permissions
- Clear audit trail of permissions

## Monitoring and Auditing

### Permission Monitoring

- All workflows explicitly declare permissions
- No workflows inherit default permissions
- Regular review of permission requirements

### Security Auditing

- CodeQL scans for missing permissions
- Regular security reviews of workflow permissions
- Documentation of permission changes

## Compliance

This configuration helps with:

- **SOC 2**: Access control and least privilege
- **ISO 27001**: Information security management
- **GDPR**: Data protection and access control
- **OWASP**: Secure development practices

## Future Considerations

### Additional Security Measures

1. **Workflow Signing**: Sign workflows for integrity
2. **Environment Protection**: Protect sensitive environments
3. **Secret Management**: Use GitHub Secrets for sensitive data
4. **Workflow Isolation**: Isolate workflows by environment

### Permission Evolution

- Regular review of permission requirements
- Remove unused permissions
- Add new permissions only when necessary
- Document all permission changes

## Troubleshooting

### Common Issues

1. **Permission Denied Errors**
   - Check if workflow has required permissions
   - Verify permission scope is correct
   - Review workflow-specific requirements

2. **Security Scan Failures**
   - Ensure security-events write permission
   - Check security workflow configuration
   - Verify security tool permissions

3. **Build Failures**
   - Verify contents read permission
   - Check artifact upload permissions
   - Review build environment access

### Debug Mode

Enable debug logging to troubleshoot permission issues:

```yaml
- name: Debug Permissions
  run: |
    echo "Current permissions:"
    echo "Contents: ${{ permissions.contents }}"
    echo "Pull-requests: ${{ permissions.pull-requests }}"
    echo "Issues: ${{ permissions.issues }}"
    echo "Checks: ${{ permissions.checks }}"
    echo "Actions: ${{ permissions.actions }}"
    echo "Security-events: ${{ permissions.security-events }}"
```

## References

- [GitHub Actions Permissions](https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#permissions)
- [CodeQL Security Rules](https://codeql.github.com/codeql-query-help/)
- [OWASP Security Guidelines](https://owasp.org/)
- [GitHub Security Best Practices](https://docs.github.com/en/code-security)
