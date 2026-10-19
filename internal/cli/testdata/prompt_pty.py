"""Run a real controlling-terminal child with a bounded prompt/no-prompt check."""
import errno
import fcntl
import os
import pty
import select
import signal
import sys
import struct
import termios
import time

prompt, *command = sys.argv[1:]
pid, master = pty.fork()
if pid == 0:
    fcntl.ioctl(1, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 100, 0, 0))
    os.execv(command[0], command)

output = b""
sent = False
deadline = time.monotonic() + 6
try:
    while True:
        if time.monotonic() > deadline:
            os.kill(pid, signal.SIGKILL)
            os.waitpid(pid, 0)
            print("PTY TIMEOUT", flush=True)
            sys.exit(124)
        if select.select([master], [], [], 0.05)[0]:
            try:
                data = os.read(master, 65536)
            except OSError as error:
                if error.errno != errno.EIO:
                    raise
                data = b""
            output += data
            sys.stdout.buffer.write(data)
            sys.stdout.buffer.flush()
            if b"\x1b[6n" in data:
                os.write(master, b"\x1b[1;1R")
            if prompt and not sent and prompt.encode() in output:
                os.write(master, os.environ.get("ANVIL_TEST_PROMPT_RESPONSE", "n\r").encode())
                sent = True
        done, status = os.waitpid(pid, os.WNOHANG)
        if done:
            if prompt and not sent:
                print("EXPECTED PROMPT MISSING", flush=True)
                sys.exit(125)
            sys.exit(os.waitstatus_to_exitcode(status))
finally:
    os.close(master)
