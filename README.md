# rfc: `pico/git` a self-hosted git server

The goal is not to create another code forge here. The goal is to create a very
simple self-hosted git solution with the ability to collaborate with external
contributors. All the code owner needs to setup a running git server:

- A single golang binary
- sqlite to store patch requests and other repo metadata

All an external contributor needs is:

- An SSH keypair
- An SSH client

# patch requests

Email is great as a decentralized system to send and receive changes (patchsets)
to a git repo. However, setting up your email client and understanding the
potential gotchas during that flow cannot be understated. Further, because we
are leveraging the email protocol for communication and collaboration, we are
limited by its feature-set. In particular, it is not possible to make edits to
emails, so if there are typos or the contributor forgot to include some code or
a line in the cover letter, there is a lot of back-and-forth noise to make those
corrections.

Further, when a contributor provides a patchset and receives feedback, the
contributor must submit another patchset via email. These are completely
separate and the only way to correlate them is via naming convention (e.g.
[PATCH] xxx v2). Now when reviewing patchsets submitted to a project, the user
must search for all versions of that particular patchset. These are separate
conversations with potentially very different patches spanning across the time
of a project. Maybe that's ok, but people are very used to a PR containing all
changes and reviews across time, and I think it makes a lot of sense to have a
single view of all conversations related to a change request.

Another issue with the email workflow is knowing how to properly reply to a
patchset. There is a lot of education on "doing patchsets right via email."

Github pull requests are easy to use, easy to edit, and easy to manage. The
downside is it forces the user to be inside their website to perform reviews.
For quick changes, this is great, but when you start reading code within a web
browser, there are quite a few downsides. At a certain point, it makes more
sense to review code inside your local development environment, IDE, etc.
Further, self-hosted solutions that mimick a pull request require a lot of
infrastructure in order to manage it. For example, before an external user who
wants to contribute to a repo, they first need to create an account and then
login. This adds quite a bit of friction for a self-hosted solution, not only
for an external contributor, but also for the code owner who has to provision
the infra.

Instead, I want to create a self-hosted git "server" that can handle sending and
receiving patches without the cumbersome nature of setting up email or the
limitations imposed by the email protocol. Further, I want the primary workflow
to surround the local development environment. Github is bringing the IDE to the
browser in order to support their workflow, I want to bring the workflow to the
local dev environment.

I see this as a hybrid between the github workflow of a pull request and sending
and receiving patches over email.

The basic idea is to leverage an SSH app to handle most of the interaction
between contributor and owner of a project. Everything can be done completely
within the terminal, in a way that is ergonomic and fully featured.

## format-patch workflow

```bash
# Owner hosts repo `noice.git` using pico/git

# Contributor clones repo
git clone git.sh:/noice.git

# Contributor wants to make a change
# Contributor makes changes via commits
git add -A && git commit -m "fix: some bugs"

# Contributor runs:
git format-patch --stdout | ssh git.sh pr noice
# (-or-) Contributor runs 
git format-patch && rsync *.patch git.sh:/noice/
# > Patch Request has been created (ID: noice/1)

# Contributor can copy down patch request metadata:
rsync git.sh:/noice/pr_1.md .
# Contributor edits patch request metadata, then runs:
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

# Contributor can checkout reviews
ssh git.sh pr noice/1 | git am -3

# Contributor/Owner could also submit a one-off comment:
rsync my_comment.md git.sh:/noice/1
# (-or-)
cat my_comment.md | git.sh comment noice/1

# rinse and repeat
```

The fundamental collaboration tool here is `format-patch`. Whether you a
submitting code changes or you are reviewing code changes, it all happens in
code. Both contributor and owner are simply creating new commits and generating
patches on top of each other. This obviates the need to have a web viewer where
the reviewing can "comment" on a line of code block. There's no need, apply the
contributor's patches, write comments or code changes, generate a new patch,
send the patch to the git server as a "review." This flow also works the exact
same if two users are collaborating on a set of changes.

This also solves the problem of sending multiple patchsets for the same code
change. There's a single, central Patch Request where all changes and
collaboration happens.

We could figure out a way to leverage `git notes` for reviews / comments, but
honestly, that solution feels brutal and outside the comfort level of most git
users. Just send reviews as code and write comments in the programming language
you are using. It's the job of the contributor to "address" those comments and
then remove them in subsequent patches.

## branch workflow

It's definitely possible for us to figure out a way to let the contributor
simply push a branch and create a patch request automatically, but there are
some rough edges to figure out there in order for it to work well.

The flow would be virtually the same as `format-patch` except the contributor
would push a branch and we would automatically create a `format-patch` against
the base branch and then funnel it into the Patch Request. We don't want
external contributors to be able to push branches into the owner's git remote
because that has all sorts of opportunities for abuse. Instead we need to figure
out how to either fork the owner's repo and let the contributor push to that
fork seamlessly, or we just generate patchsets based on the contributor's branch
and the owner's base branch.

This flow probably feels the most comfortable for Github users, but technically
more difficult to implement. Right out of the gate we need to know what base
branch the contributor wants to merge into. Then we need to figure out how to
perform reviews and followup code changes.

This feels feasible, but technically more difficult. Further, I still want to
support patchsets via `format-patch` because it is a very elegant solution for
simpler changes.

# web interface

The other idea is to make the git web viewer completely static. Whenever an
owner pushes code, we generate all the static assets for that branch. We already
have a working prototype [pgit](https://github.com/picosh/pgit) that mostly
works. Further, any patch request that gets submitted would also be statically
generated. If we don't have a web portal with authentication and the ability to
manage a repo / patch request inside the web viewer, then a static site becomes
possible. This makes the web component of a git server much simpler, since it is
just static assets. All we need to do is install a git hook that properly
generates the static assets. For patch requests, everything comes through our
SSH app so we can generate the assets when those commands are being run.

# research

- https://git-scm.com/docs/git-format-patch
- https://stackoverflow.com/a/42634501
- https://lists.sr.ht/~sircmpwn/himitsu-devel/patches/47404
