# Branch Protection Settings

This document describes the recommended branch protection settings for the `main` branch to ensure the CI/CD pipeline works correctly and code quality is maintained.

## How to Configure Branch Protection

1. Go to the repository settings (Settings tab)
2. Navigate to "Branches" in the left sidebar
3. Click "Add branch protection rule"
4. Configure the following settings:

## Recommended Branch Protection Rules for `main`

### Branch name pattern
```
main
```

### Protections
- [x] Require a pull request before merging
  - [x] Require approvals (at least 1)
  - [x] Dismiss stale pull request approvals when new commits are pushed
  - [x] Require review from Code Owners
  - [ ] Restrict who can dismiss pull request reviews

- [x] Require status checks to pass before merging
  - [x] Require branches to be up to date before merging
  - Required status checks:
    - `Lint and Test`

- [ ] Require conversation resolution before merging

- [x] Require signed commits

- [x] Require linear history

- [x] Do not allow bypassing the above settings

- [ ] Restrict who can push to matching branches

- [ ] Allow force pushes

- [ ] Allow deletions

## Benefits of These Settings

- **Quality Control**: All code changes must pass linting and tests before merge
- **Accountability**: Code reviews are required and commits must be signed
- **Clean History**: Linear history makes tracking changes and reverting easier
- **Automated Releases**: After merging to main, a new version is automatically created and published
- **Protection**: Main branch is protected from direct pushes, force pushes, and deletion 