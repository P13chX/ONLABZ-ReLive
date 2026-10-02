# ONLIVEABLE UI Brand Reference

The ReLive Technician Console follows the current ONLIVEABLE website visual system.

Source reference: `P13chX/ONLIVEABLE-Site/app/globals.css`.

## Core palette

```text
Ink / background       #050708
Panel                  #0A0D0F
Panel light            #111518
Primary text           #F4F4EF
Muted text             #A7ACAE
Hairline                rgba(255,255,255,0.14)
Signal cyan            #68EAD7
Live / critical red    #FF2D20
```

## Typography

Primary UI type follows the ONLIVEABLE Kanit-led system:

```css
font-family: "Kanit", "Noto Sans Thai", "Leelawadee UI", Arial, sans-serif;
```

Technical labels, stream IDs and timing data use a monospace stack.

No font binaries are bundled in ReLive. Deployments can provide Kanit through their own web-font strategy if desired.

## Technician Console usage

- Cyan is used for healthy signal, telemetry, chart traces and focus states.
- Red is reserved for critical incidents, failed states and the primary operational action.
- Amber remains available only as a semantic warning color for degraded/reconnecting states.
- Panels are intentionally square/technical rather than soft consumer-style cards.
- Borders use low-contrast white hairlines to match the ONLIVEABLE website.
- Large numeric telemetry keeps high contrast and tabular-number behavior.
