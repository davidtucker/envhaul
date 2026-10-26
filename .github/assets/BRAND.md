# envhaul brand

The mark is a crate stamped with `{ }` moving at speed. The crate stands for the `.env` file, the braces for the `{{ref}}` templates envhaul resolves, and the speed lines for the hauling.

## Files

| File | Use |
| --- | --- |
| `icon.svg` / `icon-512.png` | App icon on a dark rounded tile. Use the PNG for the GitHub org or user avatar. |
| `mark.svg` | The crate on its own, with no background. |
| `logo-light.svg` | Horizontal logo for light backgrounds. |
| `logo-dark.svg` | Horizontal logo for dark backgrounds. |
| `social-preview.svg` / `.png` | 1280×640 repo social preview. Upload the PNG under **Settings → General → Social preview**. |
| `banner.txt` | Unicode banner for the CLI (box-drawing and block characters). |
| `banner-ascii.txt` | Plain ASCII banner for terminals and logs without Unicode. |
| `banner-small.txt` | Three-line banner for `--version` and other tight spots. |

The wordmark is drawn as outlined paths, not set in a font, so it looks the same everywhere.

## README header

```html
<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset=".github/assets/logo-dark.svg">
    <img alt="envhaul" src=".github/assets/logo-light.svg" width="420">
  </picture>
</p>
<p align="center">Haul config and secrets from AWS into <code>.env</code> files.</p>
```

## Colors

| Role | Hex | Notes |
| --- | --- | --- |
| Amber 300 | `#FCD34D` | Top face of the crate |
| Amber 400 | `#FBBF24` | Speed lines, "haul" on dark backgrounds |
| Amber 500 | `#F59E0B` | Front face of the crate |
| Amber 600 | `#D97706` | Side face of the crate, "haul" on light backgrounds |
| Amber 700 | `#B45309` | Crate planks |
| Slate 900 | `#0F172A` | Braces, "env" on light backgrounds |
| Slate 950 | `#0B1120` | Bottom of the tile gradient |
| Slate 800 | `#1E293B` | Top of the tile gradient |
| Slate 200 | `#E2E8F0` | "env" on dark backgrounds |

In the terminal, print the crate and "haul" in amber (truecolor `#FBBF24`, or ANSI 256 color `214`) and leave "env" in the default foreground.
