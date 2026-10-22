# PathPicker

A mono path field with a Browse dialog. Protected system paths are refused.

**Status: Proposed (HCIS).** New in the HCIS specification; a reference design, not in the app.

**Implementation: awaiting authorization.** Anything here that writes to the host belongs to M2+ and needs a separately approved PR. Previews never execute operations.

Source: none in the repository.

## When to use

Use anywhere a host path is chosen (Docker permissions, shares, mounts).

## Rules

- Protected: /, /bin, /boot, /dev, /etc, /home, /lib, /proc, /root, /run, /sbin, /sys, /usr, /var, /var/lib/docker.

Props: `PathPickerProps` in `reference/index.d.ts`. Preview data is fictional and labelled “Example data”.
