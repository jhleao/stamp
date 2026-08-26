const studioDialog = document.querySelector(".studio-dialog");
const studioDialogImage = studioDialog.querySelector("img");
const studioScreenshots = [...document.querySelectorAll(".studio-open")];
let studioScreenshotIndex = 0;

function showStudioScreenshot(index) {
  studioScreenshotIndex = (index + studioScreenshots.length) % studioScreenshots.length;
  const screenshot = studioScreenshots[studioScreenshotIndex];
  studioDialogImage.src = screenshot.dataset.full;
  studioDialogImage.alt = screenshot.dataset.alt;
}

studioScreenshots.forEach((button, index) => button.addEventListener("click", () => {
  showStudioScreenshot(index);
  studioDialog.showModal();
}));

studioDialog.querySelector(".studio-previous").addEventListener("click", () => {
  showStudioScreenshot(studioScreenshotIndex - 1);
});
studioDialog.querySelector(".studio-next").addEventListener("click", () => {
  showStudioScreenshot(studioScreenshotIndex + 1);
});
studioDialog.querySelector(".studio-close").addEventListener("click", () => studioDialog.close());
studioDialog.addEventListener("click", (event) => {
  if (event.target === studioDialog) studioDialog.close();
});
studioDialog.addEventListener("keydown", (event) => {
  if (event.key === "ArrowLeft") showStudioScreenshot(studioScreenshotIndex - 1);
  if (event.key === "ArrowRight") showStudioScreenshot(studioScreenshotIndex + 1);
});
