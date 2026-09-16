# Brake-it Logo Visual Instructions

## Reference artwork

Provide this existing LapDog image to the generator as the visual reference:

```text
/Users/blezek/Source/lapdog/web/src/assets/lapdog-icon.png
```

Repository-relative path:

```text
web/src/assets/lapdog-icon.png
```

The reference is a square raster illustration with a warm cream paper-like
background, thick dark outlines, a cheerful brown dog, a yellow-orange racing
helmet, and a bright red vintage open-wheel race car. The Brake-it mark should
look like another member of that product family, not a recolored duplicate.

## Recommended concept

Show the same kind of cheerful brown racing dog wearing a yellow-orange helmet,
seen from a low three-quarter cockpit angle while pressing an oversized red
brake pedal. The dog's pose should lean slightly forward under braking. A subtle
nose-dive in the kart and two short tire-scrub marks can reinforce deceleration.

The brake pedal is the distinguishing symbol and must remain recognizable when
the image is reduced to 32 pixels. Favor one strong pedal silhouette over a
detailed pedal box or realistic footwell.

Keep these family traits:

- Friendly, energetic dog character.
- Vintage racing-cartoon proportions.
- Thick, nearly black outlines.
- Warm brown fur and cream muzzle.
- Yellow-to-orange helmet.
- Red/orange racing machinery and pedal.
- Warm cream, lightly textured background.
- Bold, simple silhouette and high local contrast.
- Square composition with comfortable padding around the subject.

Differentiate Brake-it through:

- The prominent brake pedal.
- A forward-braced braking pose rather than hands-on-wheel acceleration.
- A slight nose-down kart attitude or compressed front suspension.
- Short, controlled tire scrub marks—not smoke, a crash, or a skid out of
  control.
- A tighter crop around the dog, cockpit, front wheel, and pedal mechanism.

## Primary generation prompt

Upload the LapDog reference image, then use this prompt:

> Create a square companion logo for a racing application named Brake-it. Match
> the supplied LapDog artwork's warm vintage cartoon character design, thick dark
> outlines, soft paper texture, cheerful brown dog, yellow-orange racing helmet,
> and red/orange racing palette. Show the dog in a small vintage open-wheel kart
> from a low three-quarter cockpit angle, leaning forward under controlled
> braking while one foot clearly presses a large red brake pedal. Suggest braking
> with a slight nose-dive, compressed front suspension, and two short tire scrub
> marks. Keep the dog confident and happy. Use a bold, uncluttered silhouette
> that remains readable as a 32-pixel app icon. Warm cream background, centered
> subject, generous edge padding, no words, no letters, no numbers, no badge, no
> watermark, no photorealism.

## Alternative compositions

Generate at least four materially different compositions rather than four small
variations of one pose:

1. **Pedal close-up:** Dog and helmet above, oversized red brake pedal and paw or
   boot prominent in the lower foreground.
2. **Kart nose-dive:** Three-quarter kart view with the front suspension visibly
   compressed and the pedal still clearly readable.
3. **Cockpit cutaway:** Simplified side cutaway showing the dog pressing the
   pedal, without realistic mechanical clutter.
4. **Helmet-and-pedal emblem:** Dog's helmeted head above a single brake pedal
   and two small tire marks; the simplest option for very small use.

The emblem variant is a useful fallback if the full kart becomes illegible at
16 or 32 pixels.

## Negative instructions

Reject or regenerate images containing:

- Text, the words “Brake-it” or “LapDog,” or malformed lettering.
- Human hands or feet.
- Extra paws, legs, pedals, wheels, or duplicated controls.
- A frightened, injured, angry, or crashing dog.
- Heavy tire smoke, flames, collision damage, or dangerous imagery.
- A modern photographic race car or photorealistic fur.
- A generic dog unrelated to the reference character family.
- Dark or visually busy backgrounds.
- Thin outlines or low contrast that disappear at icon size.
- The number `1`, which belongs to the current LapDog car and is not needed for
  Brake-it.
- A composition cropped tightly enough to cut off the helmet, pedal, or wheels.

## Suggested web applications

### ChatGPT Images

<https://chatgpt.com/>

Good first choice for conversational iteration and direct edits. Upload the
LapDog image, use the primary prompt, then ask for isolated corrections such as
“make the red brake pedal twice as large” or “simplify the silhouette for a
32-pixel icon.” Official usage guidance:
<https://help.openai.com/en/articles/11084440-images-in-chatgpt>

### Adobe Firefly

<https://firefly.adobe.com/>

Useful when explicit composition and style-reference strength controls are
helpful. Upload the LapDog artwork as a style reference; use a simple sketch as
the composition reference if the pedal placement keeps drifting. Official
reference-image guidance:
<https://helpx.adobe.com/firefly/web/work-with-images/generate-images/match-image-composition-to-reference-image.html>

### Midjourney Web

<https://www.midjourney.com/>

Useful for producing polished cartoon variations. Add the LapDog image as a
style or character/object reference, then compare several compositions before
refining one. Official web creation guidance:
<https://docs.midjourney.com/hc/en-us/articles/33390732264589-Creating-on-Web>

### Ideogram

<https://ideogram.ai/>

Useful for graphic, logo-like compositions and remixing an uploaded reference.
Reference-image upload and style-reference features may require a paid plan.
Official upload guidance:
<https://docs.ideogram.ai/canvas-and-editing/image-upload>

Before uploading the LapDog artwork, review the selected service's current
privacy, public-gallery, training-use, and commercial-use settings. Prefer a
private generation mode where available. Retain the prompt and generation
details with the chosen master so the design can be reproduced or revised.

## Selection checklist

Inspect every candidate at these sizes:

- **1024×1024:** line quality, anatomy, artifacts, and consistency with LapDog.
- **160×160:** README and larger application branding.
- **32×32:** sidebar and Brake-it header.
- **16×16:** worst-case favicon or compact browser use.

Select only a candidate for which all of these are true:

- The dog reads immediately as related to LapDog.
- The brake pedal reads immediately as the action and differentiator.
- Braking looks controlled rather than accidental.
- The silhouette remains understandable at 32×32.
- No important detail depends on subtle texture or tiny lines.
- The image has no unwanted text, watermark, anatomy error, or duplicated part.
- The crop leaves room for circular or rounded-square presentation.

## Finishing and deliverables

1. Correct small anatomy, outline, and pedal-shape defects in an image editor.
2. Preserve the untouched generated master separately from the cleaned master.
3. Export a color-managed 1024×1024 PNG as the source asset.
4. Produce optimized 160×160, 32×32, and 16×16 PNG previews using a high-quality
   downsampler; do not judge small-size legibility by browser scaling alone.
5. Inspect the small exports on both light and dark surrounding surfaces.
6. When approved, store the master in `web/src/assets/` with a descriptive name
   such as `brake-it-icon.png` and use the same asset in both cross-application
   navigation links.
7. If Brake-it and LapDog become a broader product family, commission a vector
   redraw of both raster marks from one illustrator to establish consistent
   production masters, outlines, and color values.

## Selected artwork

The pedal close-up was selected for the compact product mark because its dog,
helmet, and red pedal remain distinct in the sidebar and Brake-It header. The
full-kart composition is used as supporting simulator artwork, where its extra
detail has enough room to remain readable. Optimized application assets are:

```text
web/src/assets/brake-it-icon.png
web/src/assets/brake-it-kart.png
```

The original 1254×1254 generated PNGs remain the source masters:

```text
ChatGPT Image Sep 16, 2026, 08_39_59 AM.png
ChatGPT Image Sep 16, 2026, 08_51_28 AM.png
```

The application copies are stripped, resized PNGs so embedding the artwork does
not add both full-resolution masters to each executable.
