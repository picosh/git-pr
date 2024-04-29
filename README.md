# patch requests

## format-patch

```bash
# Owner hosts repo `noice.git` on pico/git

# User clones repo
git clone git.sh:/noice.git

# User wants to make a change
# User makes changes via commits
git add -A && git commit -m "fix: some bugs"

# User runs:
git format-patch --stdout | ssh git.sh pr noice
# (-or-) User runs 
git format-patch && rsync *.patch git.sh:/noice/
# > Patch Request has been created (ID: noice/1)

# User can copy down patch request metadata:
rsync git.sh:/noice/pr_1.md .
# User edits patch request metadata, then runs:
rsync pr_1.md git.sh:/noice/

# Owner can checkout patch:
ssh git.sh pr noice/1 | git am -3
# Owner can comment in code, then commit, then send another format-patch
# on top of it:
git format-patch --stdout | ssh git.sh pr noice/1
# We do some magic in the UI to make this look like comments or someway to
# clearly mark as a review

# Owner can reject a pr:
ssh git.sh pr noice/1 --close
# (-maybe-) Owner can merge a pr:
ssh git.sh pr noice/1 --squash-n-merge

# User can checkout reviews
ssh git.sh pr noice/1 | git am -3

# rinse and repeat
```

# research

- https://git-scm.com/docs/git-format-patch
- https://stackoverflow.com/a/42634501
- https://lists.sr.ht/~sircmpwn/himitsu-devel/patches/47404
