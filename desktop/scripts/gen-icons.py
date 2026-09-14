#!/usr/bin/env python3
"""Generate a simple amber-B icon set for the BuildBee desktop shell."""

from __future__ import annotations

import struct
import zlib
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1] / "src-tauri" / "icons"


def png_rgba(width: int, height: int, pixels: bytes) -> bytes:
    def chunk(tag: bytes, data: bytes) -> bytes:
        return (
            struct.pack(">I", len(data))
            + tag
            + data
            + struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF)
        )

    raw = b""
    stride = width * 4
    for y in range(height):
        raw += b"\x00" + pixels[y * stride : (y + 1) * stride]
    return (
        b"\x89PNG\r\n\x1a\n"
        + chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 6, 0, 0, 0))
        + chunk(b"IDAT", zlib.compress(raw, 9))
        + chunk(b"IEND", b"")
    )


def rounded_rect(px: list[int], w: int, h: int, color: tuple[int, int, int, int], radius: int) -> None:
    r2 = radius * radius
    for y in range(h):
        for x in range(w):
            inside = True
            if x < radius and y < radius and (x - radius) ** 2 + (y - radius) ** 2 > r2:
                inside = False
            if x >= w - radius and y < radius and (x - (w - 1 - radius)) ** 2 + (y - radius) ** 2 > r2:
                inside = False
            if x < radius and y >= h - radius and (x - radius) ** 2 + (y - (h - 1 - radius)) ** 2 > r2:
                inside = False
            if (
                x >= w - radius
                and y >= h - radius
                and (x - (w - 1 - radius)) ** 2 + (y - (h - 1 - radius)) ** 2 > r2
            ):
                inside = False
            if inside:
                i = (y * w + x) * 4
                px[i : i + 4] = list(color)


def letter_b(px: list[int], size: int) -> None:
    # Very small bitmap for "B" in the center, scaled to the canvas.
    glyph = [
        "111110",
        "100001",
        "100001",
        "111110",
        "100001",
        "100001",
        "111110",
    ]
    gh, gw = len(glyph), len(glyph[0])
    scale = max(1, size // 16)
    total_w = gw * scale
    total_h = gh * scale
    ox = (size - total_w) // 2
    oy = (size - total_h) // 2
    ink = (24, 24, 27, 255)  # zinc-950
    for gy, row in enumerate(glyph):
        for gx, ch in enumerate(row):
            if ch != "1":
                continue
            for dy in range(scale):
                for dx in range(scale):
                    x, y = ox + gx * scale + dx, oy + gy * scale + dy
                    if 0 <= x < size and 0 <= y < size:
                        i = (y * size + x) * 4
                        px[i : i + 4] = list(ink)


def make(size: int) -> bytes:
    px = [0] * (size * size * 4)
    pad = max(1, size // 16)
    inner = size - pad * 2
    # Draw onto a temp buffer then blit with padding.
    tmp = [0] * (inner * inner * 4)
    rounded_rect(tmp, inner, inner, (251, 191, 36, 255), max(2, inner // 6))  # amber-400
    letter_b(tmp, inner)
    for y in range(inner):
        for x in range(inner):
            si = (y * inner + x) * 4
            di = ((y + pad) * size + (x + pad)) * 4
            px[di : di + 4] = tmp[si : si + 4]
    return png_rgba(size, size, bytes(px))


def ico(pngs: list[tuple[int, bytes]]) -> bytes:
    # PNG-compressed ICO (Vista+).
    count = len(pngs)
    header = struct.pack("<HHH", 0, 1, count)
    entries = b""
    payload = b""
    offset = 6 + 16 * count
    for size, data in pngs:
        w = 0 if size >= 256 else size
        entries += struct.pack("<BBBBHHII", w, w, 0, 0, 1, 32, len(data), offset)
        payload += data
        offset += len(data)
    return header + entries + payload


def icns(images: dict[bytes, bytes]) -> bytes:
    body = b""
    for tag, data in images.items():
        body += tag + struct.pack(">I", 8 + len(data)) + data
    return b"icns" + struct.pack(">I", 8 + len(body)) + body


def main() -> None:
    ROOT.mkdir(parents=True, exist_ok=True)
    png32 = make(32)
    png128 = make(128)
    png256 = make(256)
    png512 = make(512)
    (ROOT / "32x32.png").write_bytes(png32)
    (ROOT / "128x128.png").write_bytes(png128)
    (ROOT / "256x256.png").write_bytes(png256)
    (ROOT / "512x512.png").write_bytes(png512)
    (ROOT / "icon.png").write_bytes(png512)
    (ROOT / "128x128@2x.png").write_bytes(png256)
    (ROOT / "icon.ico").write_bytes(ico([(32, png32), (256, png256)]))
    (ROOT / "icon.icns").write_bytes(
        icns(
            {
                b"ic07": png128,  # 128
                b"ic08": png256,  # 256
                b"ic09": png512,  # 512
            }
        )
    )
    print(f"wrote icons in {ROOT}")


if __name__ == "__main__":
    main()
