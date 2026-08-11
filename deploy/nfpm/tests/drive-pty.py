"""Run <app>-uninstall on a real pty and type one answer at its prompt.

The interactive confirmation is the gate in front of an irreversible wipe, so it has to be
exercised for real rather than reasoned about: `[ -t 0 ]` is false under a plain pipe, and
the branch would never run.

Usage: drive-pty.py <app> <answer> [uninstall args...]
"""
import os
import pty
import sys

app = sys.argv[1]
answer = sys.argv[2].encode() + b"\n"
argv = ["/usr/sbin/%s-uninstall" % app] + sys.argv[3:]
sent = [False]


def stdin_read(fd):
    # Hand over the answer once, then report EOF. On Python >= 3.8 that only drops stdin
    # from the copy loop and leaves the pty master open, so the child runs to completion
    # instead of being hung up on halfway through removing the package.
    if not sent[0]:
        sent[0] = True
        return answer
    return b""


def master_read(fd):
    # pty.spawn calls this with the fd only — passing os.read directly raises TypeError
    # mid-copy, which tears the pty down at a moment that depends on scheduling. That
    # makes the whole suite flaky rather than failing: the child sometimes has already
    # read the answer and completes anyway. Read explicitly instead.
    return os.read(fd, 1024)


sys.exit(0 if os.WIFEXITED(pty.spawn(argv, master_read, stdin_read)) else 1)
