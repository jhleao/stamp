#!/usr/bin/env python3
"""Build Stamp's narrated quick-start video from verified product screens."""

from __future__ import annotations

import json
import subprocess
import tempfile
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont


ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "docs" / "tutorial"
SIZE = (1280, 720)
PAPER = "#f7f3ed"
INK = "#29231f"
MUTED = "#746a62"
ACCENT = "#c74732"
FONT = "/System/Library/Fonts/Avenir Next.ttc"
SERIF = "/System/Library/Fonts/NewYork.ttf"

SCENES = [
    {
        "eyebrow": "STAMP · QUICK START",
        "title": "Make document packs\nwith people and agents",
        "body": "One local workspace. One shared Drive version.\nPull - edit - preview - push.",
        "narration": "Welcome to Stamp. Stamp is a small Mac utility for making polished document packs with people and coding agents. The whole mental model is pull, edit, preview, and push. Local work stays private until you share a complete, recoverable version to Google Drive.",
    },
    {
        "eyebrow": "1 · SET UP ONCE",
        "title": "Install the renderer wheels",
        "code": "# Install a binary from github.com/jhleao/stamp/releases\nstamp doctor\n# Source contributors: npm ci && make install",
        "body": "The installed app is one binary. Tailwind is only needed while\nauthoring themes; the shared project contains inert compiled CSS.",
        "narration": "First, install Stamp and its rendering and authoring tools. The installed Stamp app is one binary. Chrome produces PDFs, LibreOffice handles spreadsheets and presentation files, Pandoc supports document compatibility, and Tailwind compiles themes into inert local CSS.",
    },
    {
        "eyebrow": "2 · CONNECT GOOGLE DRIVE",
        "title": "One login, ordinary projects",
        "code": "stamp doctor && stamp login",
        "body": "Each project is its own shared Drive folder.\nDrive supplies permissions, history, and recovery.",
        "narration": "Next, verify the setup and sign in to Google Drive. Every Stamp project becomes its own ordinary shared Drive folder. Stamp does not add a server or database. Drive supplies team permissions, revision history, and recovery.",
    },
    {
        "eyebrow": "3 · MAKE A REUSABLE LOOK",
        "title": "Each project includes a theme folder",
        "code": "# Open Templates in Studio\n# Edit components and Tailwind\n# Save and inspect the preview",
        "body": "HTML frames · Tailwind · local assets · examples\ncomponents/metric-card.tsx -> <metric-card>",
        "narration": "To make your own visual system, modify the project theme. Each project includes a theme folder containing HTML frames, a Tailwind design system, local assets, reusable components, and examples. There is no package manager or install step. A component file named metric card becomes a metric card tag that stays visually simple inside Markdown.",
    },
    {
        "eyebrow": "4 · LET THE AGENT SHAPE IT",
        "title": "Start an agent session\nfrom Studio",
        "body": "The robot button copies the workspace path and asks the\nagent to run stamp skill before editing.",
        "code": "stamp new board-pack \\\n+  --name 'Board Pack'\ncd board-pack && stamp studio",
        "narration": "Open Studio and use the robot button to copy a ready-to-paste agent prompt. It includes the workspace path and asks the agent to run Stamp skill before editing. Then describe the result you want: match the brand, reuse existing components, add stress test examples, and inspect every affected preview.",
    },
    {
        "eyebrow": "5 · WRITE BESIDE THE RESULT",
        "title": "Content stays readable",
        "image": "studio.png",
        "body": "Studio opens real content immediately. Monaco helps with\nMarkdown, HTML, formatting, and Tailwind utility classes.",
        "narration": "Studio opens a real starter document immediately, with source on the left and the output on the right. Monaco helps with Markdown, TSX, HTML, formatting, and Tailwind utility classes. Save deliberately when a draft is ready to render. Edits made by your coding agent appear automatically when the editor is idle and never overwrite an active draft. Components keep layout out of the prose, so a document remains easy to read and hand to someone else.",
    },
    {
        "eyebrow": "6 · SHARE A VERSION",
        "title": "One note, one recoverable push",
        "code": "stamp push \\\n+  --message 'first useful draft'",
        "body": "Push renders everything first, then updates the canonical\n.stamp archive and friendly PDF/XLSX mirrors.",
        "narration": "When the pack is ready, choose Push and add a short version note, or run Stamp push. Stamp renders everything before touching Drive, updates one canonical archive, and mirrors friendly PDF and spreadsheet outputs. Every push is a Drive revision you can recover.",
    },
    {
        "eyebrow": "7 · COLLABORATE WITHOUT SURPRISES",
        "title": "Pull when an update is ready",
        "code": "stamp clone board-pack\nstamp studio\n# Later: Pull lights up when Drive is newer\nstamp push --message 'tighten the narrative'",
        "body": "If local and shared work both changed, Stamp keeps a recovery\ncopy and asks before replacing. Nothing is silently discarded.",
        "narration": "A teammate runs Stamp clone and explicitly chooses the shared project in Google Picker. On later visits, Pull lights up when Drive is newer. They edit, preview, and push again. If local work and Drive both changed, Stamp stops and makes a recovery copy before replacement. That is the complete collaboration model: ordinary files locally, and deliberate, recoverable versions in Drive.",
    },
    {
        "eyebrow": "THAT’S STAMP",
        "title": "Pull - edit - preview - push",
        "body": "Readable Markdown. Powerful themes. Safe agent help.\nNo server, database, package graph, or instruction manual.",
        "narration": "That is Stamp: readable Markdown, powerful file based themes, safe coding agent help, and a collaboration loop anyone can explain. Pull, edit, preview, and push.",
    },
]


def font(size: int, serif: bool = False, index: int = 0) -> ImageFont.FreeTypeFont:
    path = SERIF if serif and Path(SERIF).exists() else FONT
    return ImageFont.truetype(path, size, index=index)


def wrap(draw: ImageDraw.ImageDraw, text: str, face: ImageFont.FreeTypeFont, width: int) -> list[str]:
    lines: list[str] = []
    for paragraph in text.splitlines():
        words = paragraph.split()
        current = ""
        for word in words:
            candidate = f"{current} {word}".strip()
            if draw.textlength(candidate, font=face) <= width:
                current = candidate
            else:
                if current:
                    lines.append(current)
                current = word
        lines.append(current)
    return lines


def draw_scene(scene: dict[str, str], destination: Path) -> None:
    canvas = Image.new("RGB", SIZE, PAPER)
    draw = ImageDraw.Draw(canvas)
    draw.rectangle((0, 0, 14, SIZE[1]), fill=ACCENT)
    draw.text((70, 56), scene["eyebrow"], font=font(17), fill=ACCENT)

    title_face = font(46 if scene.get("image") else 58, serif=True)
    y = 105
    for line in scene["title"].splitlines():
        draw.text((70, y), line, font=title_face, fill=INK)
        y += 67

    if image_name := scene.get("image"):
        screenshot = Image.open(OUT / image_name).convert("RGB")
        screenshot.thumbnail((740, 390), Image.Resampling.LANCZOS)
        x = SIZE[0] - screenshot.width - 55
        y_image = 180
        draw.rectangle((x - 8, y_image - 8, x + screenshot.width + 8, y_image + screenshot.height + 8), fill="#ddd5cc")
        canvas.paste(screenshot, (x, y_image))
        body_x, body_y, body_width = 70, 440, 360
    else:
        body_x, body_y, body_width = 70, max(y + 28, 315), 1080

    if code := scene.get("code"):
        code = code.replace("\n+  ", "\n  ")
        code_face = font(24)
        code_lines = code.splitlines()
        box_top = body_y
        box_height = 34 * len(code_lines) + 42
        draw.rounded_rectangle((70, box_top, 1210, box_top + box_height), radius=8, fill=INK)
        for i, line in enumerate(code_lines):
            draw.text((96, box_top + 20 + i * 34), line, font=code_face, fill="#fffaf3")
        body_y = box_top + box_height + 28

    body_face = font(25)
    for line in wrap(draw, scene["body"], body_face, body_width):
        draw.text((body_x, body_y), line, font=body_face, fill=MUTED)
        body_y += 34

    draw.line((70, 666, 1210, 666), fill="#d8d0c7", width=1)
    draw.text((70, 678), "stamp · document packs with people and agents", font=font(14), fill=MUTED)
    destination.parent.mkdir(parents=True, exist_ok=True)
    canvas.save(destination, quality=95)


def duration(path: Path) -> float:
    output = subprocess.check_output([
        "ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "json", str(path)
    ])
    return float(json.loads(output)["format"]["duration"])


def main() -> None:
    OUT.mkdir(parents=True, exist_ok=True)
    final = OUT / "stamp-tutorial.mp4"
    with tempfile.TemporaryDirectory(prefix="stamp-tutorial-") as temp_name:
        temp = Path(temp_name)
        clips: list[Path] = []
        for index, scene in enumerate(SCENES, start=1):
            image_path = temp / f"scene-{index:02}.png"
            audio_path = temp / f"scene-{index:02}.aiff"
            clip_path = temp / f"scene-{index:02}.mp4"
            draw_scene(scene, image_path)
            subprocess.run(["say", "-v", "Samantha", "-r", "185", "-o", str(audio_path), scene["narration"]], check=True)
            seconds = duration(audio_path) + 0.45
            fade_out = max(0.1, seconds - 0.35)
            subprocess.run([
                "ffmpeg", "-loglevel", "error", "-y", "-loop", "1", "-i", str(image_path), "-i", str(audio_path),
                "-vf", f"fade=t=in:st=0:d=0.25,fade=t=out:st={fade_out}:d=0.35,format=yuv420p",
                "-af", f"afade=t=in:st=0:d=0.15,afade=t=out:st={max(0.1, seconds - .25)}:d=0.25",
                "-t", f"{seconds:.3f}", "-r", "24", "-c:v", "libx264", "-preset", "ultrafast", "-crf", "22",
                "-c:a", "aac", "-b:a", "160k", "-shortest", str(clip_path),
            ], check=True)
            clips.append(clip_path)
        concat = temp / "clips.txt"
        concat.write_text("".join(f"file '{path}'\n" for path in clips))
        subprocess.run([
            "ffmpeg", "-loglevel", "error", "-y", "-f", "concat", "-safe", "0", "-i", str(concat),
            "-c", "copy", "-movflags", "+faststart", "-metadata", "title=Stamp quick start", str(final),
        ], check=True)
    print(final)


if __name__ == "__main__":
    main()
