/**
 * Copy text to clipboard with fallback for non-secure contexts
 * @param {string} text - Text to copy
 * @returns {Promise<boolean>} - True if successful, false otherwise
 */
export async function copyToClipboard(text) {
	// Try modern Clipboard API first (works in secure contexts: HTTPS, localhost)
	if (navigator.clipboard && window.isSecureContext) {
		try {
			await navigator.clipboard.writeText(text);
			return true;
		} catch (err) {
			console.warn('Clipboard API failed, trying fallback:', err);
		}
	}

	// Fallback: Use document.execCommand('copy') with a temporary textarea
	return fallbackCopyToClipboard(text);
}

/**
 * Fallback method using execCommand
 * @param {string} text - Text to copy
 * @returns {boolean} - True if successful, false otherwise
 */
function fallbackCopyToClipboard(text) {
	const textarea = document.createElement('textarea');
	textarea.value = text;

	// Make the textarea invisible and positioned off-screen
	textarea.style.position = 'fixed';
	textarea.style.left = '-999999px';
	textarea.style.top = '-999999px';
	document.body.appendChild(textarea);

	// Select and copy
	textarea.focus();
	textarea.select();

	let successful = false;
	try {
		successful = document.execCommand('copy');
	} catch (err) {
		console.error('Fallback copy failed:', err);
	}

	// Clean up
	document.body.removeChild(textarea);

	return successful;
}
