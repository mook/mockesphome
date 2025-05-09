import asyncio
from collections.abc import Awaitable, Callable
import contextlib
import functools
import os
import pathlib
import signal
import socket
import subprocess
import sys
import tempfile
import typing
import unittest

# aioesphomeapi doesn't do exports the way pyright wants
# pyright: reportPrivateImportUsage=false
import aioesphomeapi

def config[T](config_file: str):
  """ Decorator to run the application with the given config file."""
  integration_dir = pathlib.Path(__file__).parent
  project_dir = integration_dir.parent.resolve(True)
  resolved_config = (integration_dir / config_file).resolve(True)
  def wrapper(fn: Callable[[T], Awaitable[None]]):
    @functools.wraps(fn)
    async def wrapped(self: T):
      with contextlib.ExitStack() as stack:
        # Set up a systemd notify socket so we know when the app is actually
        # running (instead of `go run` still compiling).
        work_dir = stack.push(tempfile.TemporaryDirectory(prefix="habp-test-"))
        socket_path = str(pathlib.Path(work_dir.name) / "sd-notify.sock")
        queue = asyncio.Queue[None]()
        class protocol(asyncio.DatagramProtocol):
          @typing.override
          def datagram_received(self, data: bytes, addr: tuple[str | typing.Any, int]) -> None:
            queue.put_nowait(None)
        loop = asyncio.get_event_loop()
        (transport, _) = await loop.create_datagram_endpoint(protocol, local_addr=socket_path, family=socket.AF_UNIX)
        _ = stack.push(contextlib.closing(transport))
        # Run the application; use a process group so that we can kill the
        # actual process (instead of the `go run` process).
        args = ["go", "run", ".", "-config", resolved_config]
        if ("-v" in sys.argv) or ("--verbose" in sys.argv):
          args.append("--verbose")
        proc = subprocess.Popen(
          args,
          cwd=project_dir,
          env=os.environ | {"NOTIFY_SOCKET": socket_path},
          preexec_fn=os.setsid)
        try:
          async with asyncio.timeout(30):
            await queue.get()
          return await fn(self)
        finally:
          os.killpg(proc.pid, signal.SIGTERM)
          try:
            _ = proc.wait(5)
          except subprocess.TimeoutExpired:
            os.killpg(proc.pid, signal.SIGKILL)
    return wrapped
  return wrapper

class TestHello(unittest.IsolatedAsyncioTestCase):
  @config("hello.yaml")
  async def test_hello(self):
    api = aioesphomeapi.APIClient("localhost", 6053, password=None)
    await api.connect()
    self.assertIsNotNone(api.api_version)
    await api.disconnect()

  @config("auth.yaml")
  async def test_hello_login(self):
    api = aioesphomeapi.APIClient("localhost", 6053, password="hunter2")
    await api.connect(login=True)
    self.assertIsNotNone(api.api_version)
    await api.disconnect()

  @config("port.yaml")
  async def test_hello_port(self):
    api = aioesphomeapi.APIClient("localhost", 49284, password=None)
    await api.connect()
    self.assertIsNotNone(api.api_version)
    await api.disconnect()

  @config("hello.yaml")
  async def test_device_info(self):
    api = aioesphomeapi.APIClient("localhost", 6053, password=None)
    await api.connect()
    self.assertIsNotNone(api.api_version)
    info = await api.device_info()
    self.assertFalse(info.uses_password)
    self.assertRegex(info.mac_address, r"^(?:[0-9a-f]{2}:){5}[0-9a-f]{2}$")
