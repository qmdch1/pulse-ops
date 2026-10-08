# Pulse Ops development workflow

- The user-designated GitHub repository is `https://github.com/qmdch1/pulse-ops.git`.
- After each requested feature or fix passes its relevant tests, commit the verified changes and push them to this repository. Do not leave completed, tested work only in the local checkout.
- Use `main` unless the user specifies another branch. Preserve existing history; do not force-push. Inspect and reconcile remote changes before publishing if the remote branch has advanced.
- Run checks appropriate to the changed behavior. Report what was actually verified and any remaining limitations; do not describe untested integrations as complete.
- Keep credentials, private keys, local SSH inventories, runtime data, generated binaries, and local environment files out of Git. Use placeholder example configuration for production connection settings.
- After pushing, verify that the remote branch points to the expected commit and report the commit and repository link to the user.
- This is a development workflow, not a request to create a scheduled automation or to deploy to production.
