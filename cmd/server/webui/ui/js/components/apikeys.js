import { api } from '../api.js';
import { state } from '../state.js';
import { notifications } from '../utils/notifications.js';
import { getStatusBadge } from '../utils/formatters.js';
import { t } from '../utils/i18n.js';

class APIKeys {
	constructor() {
		this.container = document.getElementById('view-container');
		this.apiKeys = [];
		this.endpoints = [];
		this.config = { enabled: false };
		// 监听语言切换
		window.addEventListener('languageChanged', () => {
			if (state.get('currentView') === 'apikeys') {
				this.render();
			}
		});
	}

	async render() {
		this.container.innerHTML = `
			<div class="apikeys">
				<div class="apikeys-header">
					<h1>${t('apikeys.title')}</h1>
					<p class="subtitle">${t('apikeys.authEnabledHelp')}</p>
					<div class="header-actions">
						<button class="btn btn-secondary" id="apikeys-config-btn">
							⚙️ ${t('apikeys.authEnabled')}
						</button>
						<button class="btn btn-primary" id="add-apikey-btn">
							<span>+ ${t('apikeys.addKey')}</span>
						</button>
					</div>
				</div>

				<div class="auth-card" id="auth-status-card">
					<div class="auth-card-info">
						<div class="auth-status-indicator disabled" id="auth-icon-indicator">
							🔓
							<span class="pulse-dot"></span>
						</div>
						<div class="auth-card-text">
							<h3>${t('apikeys.authEnabled')}</h3>
							<p id="auth-description">${t('apikeys.authEnabledHelp')}</p>
						</div>
					</div>
					<div class="auth-status-badge">
						<span id="apikeys-auth-status" class="badge badge-secondary">${t('common.disabled')}</span>
					</div>
				</div>

				<div class="apikeys-table-card">
					<div class="apikeys-table-header">
						<h3>${t('apikeys.keyValue')}</h3>
						<span class="apikeys-count" id="apikeys-total-count">0</span>
					</div>
					<div class="card-body">
						<div id="apikeys-table"></div>
					</div>
				</div>
			</div>
		`;

		document.getElementById('add-apikey-btn').addEventListener('click', () => this.showAddModal());
		document.getElementById('apikeys-config-btn').addEventListener('click', () => this.showConfigModal());

		await this.loadAPIKeys();
		await this.loadConfig();
	}

	async loadAPIKeys() {
		try {
			const data = await api.getAPIKeys();
			this.apiKeys = Array.isArray(data) ? data : [];
			this.renderTable();
		} catch (error) {
			notifications.error(`${t('apikeys.failedToLoad')}: ${error.message}`);
		}
	}

	async loadConfig() {
		try {
			const data = await api.getAPIKeysConfig();
			this.config = data;
			this.updateAuthStatus();
		} catch (error) {
			console.error('Failed to load API keys config:', error);
		}
	}

	updateAuthStatus() {
		const statusEl = document.getElementById('apikeys-auth-status');
		const indicatorEl = document.getElementById('auth-icon-indicator');
		if (statusEl) {
			statusEl.textContent = this.config.enabled ? t('common.enabled') : t('common.disabled');
			statusEl.className = this.config.enabled ? 'badge badge-success' : 'badge badge-secondary';
		}
		if (indicatorEl) {
			if (this.config.enabled) {
				indicatorEl.className = 'auth-status-indicator enabled';
				indicatorEl.innerHTML = '🔐<span class="pulse-dot"></span>';
			} else {
				indicatorEl.className = 'auth-status-indicator disabled';
				indicatorEl.innerHTML = '🔓<span class="pulse-dot"></span>';
			}
		}
	}

	renderTable() {
		const container = document.getElementById('apikeys-table');
		const countEl = document.getElementById('apikeys-total-count');

		if (countEl) {
			countEl.textContent = this.apiKeys.length;
		}

		if (this.apiKeys.length === 0) {
			container.innerHTML = `
				<div class="empty-state">
					<div class="empty-state-icon-wrapper">
						<span class="empty-state-icon">🔑</span>
					</div>
					<div class="empty-state-title">${t('apikeys.noKeys')}</div>
					<div class="empty-state-message">${t('apikeys.noKeysMessage')}</div>
					<button class="empty-state-action" id="empty-add-btn">
						<span>+</span> ${t('apikeys.addKey')}
					</button>
				</div>
			`;
			document.getElementById('empty-add-btn').addEventListener('click', () => this.showAddModal());
			return;
		}

		container.innerHTML = `
			<div class="table-container">
				<table class="table apikeys-table">
					<thead>
						<tr>
							<th>${t('common.name')}</th>
							<th>${t('apikeys.keyValue')}</th>
							<th>${t('apikeys.endpoints')}</th>
							<th>${t('apikeys.expiresAt')}</th>
							<th>${t('apikeys.lastUsed')}</th>
							<th>${t('common.status')}</th>
							<th>${t('common.actions')}</th>
						</tr>
					</thead>
					<tbody>
						${this.apiKeys.map(key => this.renderKeyRow(key)).join('')}
					</tbody>
				</table>
			</div>
		`;

		this.attachEventListeners();
	}

	renderKeyRow(key) {
		const maskedKey = this.maskKey(key.keyValue);
		const expiresText = key.expiresAt ? new Date(key.expiresAt).toLocaleString() : t('apikeys.never');
		const lastUsedText = key.lastUsedAt ? new Date(key.lastUsedAt).toLocaleString() : '-';
		const expiresClass = key.expiresAt && new Date(key.expiresAt) < new Date() ? 'expired' : (key.expiresAt ? '' : 'never');

		return `
			<tr data-key-id="${key.id}">
				<td>
					<span class="key-name">${this.escapeHtml(key.name)}</span>
				</td>
				<td>
					<div class="key-value-cell">
						<code class="key-value-display">${maskedKey}</code>
						<button class="copy-key-btn" data-key="${this.escapeHtml(key.keyValue)}" title="${t('apikeys.copyKey')}">
							📋
						</button>
					</div>
				</td>
				<td>
					<div class="endpoint-tags">
						${key.endpointNames.map(name => `<span class="endpoint-tag">${this.escapeHtml(name)}</span>`).join('')}
					</div>
				</td>
				<td><span class="time-display ${expiresClass}">${expiresText}</span></td>
				<td><span class="time-display">${lastUsedText}</span></td>
				<td>${getStatusBadge(key.enabled)}</td>
				<td>
					<div class="action-buttons">
						<button class="action-btn edit-btn" data-id="${key.id}" title="${t('common.edit')}">
							✏️
						</button>
						<button class="action-btn regenerate-btn" data-id="${key.id}" title="${t('apikeys.regenerateKey')}">
							🔄
						</button>
						<button class="action-btn delete-btn" data-id="${key.id}" title="${t('common.delete')}">
							🗑️
						</button>
					</div>
				</td>
			</tr>
		`;
	}

	maskKey(key) {
		if (key.length <= 8) return key;
		return key.substring(0, 8) + '...' + key.substring(key.length - 4);
	}

	escapeHtml(str) {
		const div = document.createElement('div');
		div.textContent = str;
		return div.innerHTML;
	}

	attachEventListeners() {
		// Copy key buttons
		document.querySelectorAll('.copy-key-btn').forEach(btn => {
			btn.addEventListener('click', () => {
				const key = btn.dataset.key;
				navigator.clipboard.writeText(key).then(() => {
					notifications.success(t('apikeys.keyCopied'));
				}).catch(() => {
					notifications.error('Failed to copy key');
				});
			});
		});

		// Edit buttons
		document.querySelectorAll('.edit-btn').forEach(btn => {
			btn.addEventListener('click', () => {
				const id = parseInt(btn.dataset.id);
				this.showEditModal(id);
			});
		});

		// Regenerate buttons
		document.querySelectorAll('.regenerate-btn').forEach(btn => {
			btn.addEventListener('click', () => {
				const id = parseInt(btn.dataset.id);
				this.regenerateKey(id);
			});
		});

		// Delete buttons
		document.querySelectorAll('.delete-btn').forEach(btn => {
			btn.addEventListener('click', () => {
				const id = parseInt(btn.dataset.id);
				this.deleteKey(id);
			});
		});
	}

	async showAddModal() {
		await this.loadEndpoints();

		const modal = document.createElement('div');
		modal.className = 'modal-overlay apikeys-modal';
		modal.innerHTML = `
			<div class="modal">
				<div class="modal-header">
					<h3>${t('apikeys.addKey')}</h3>
					<button class="modal-close" id="close-modal">×</button>
				</div>
				<div class="modal-body">
					<form id="add-apikey-form">
						<div class="form-group">
							<label class="form-label">${t('apikeys.keyName')} *</label>
							<input type="text" name="name" class="form-control" required placeholder="${t('apikeys.keyName')}">
						</div>
						<div class="form-group">
							<label class="form-label">${t('apikeys.endpoints')} *</label>
							<div class="endpoint-checkboxes">
								${this.endpoints.map(ep => `
									<label class="checkbox-label">
										<input type="checkbox" name="endpoints" value="${this.escapeHtml(ep.name)}">
										${this.escapeHtml(ep.name)}
									</label>
								`).join('')}
							</div>
						</div>
						<div class="form-group">
							<label class="form-label">${t('apikeys.expiresAt')}</label>
							<input type="datetime-local" name="expiresAt" class="form-control">
							<small class="text-muted">${t('apikeys.never')}</small>
						</div>
						<div class="form-group">
							<label class="checkbox-label">
								<input type="checkbox" name="enabled" checked>
								${t('common.enabled')}
							</label>
						</div>
						<div class="modal-footer">
							<button type="button" class="btn btn-secondary" id="cancel-btn">${t('common.cancel')}</button>
							<button type="submit" class="btn btn-primary">${t('common.create')}</button>
						</div>
					</form>
				</div>
			</div>
		`;

		document.body.appendChild(modal);

		modal.querySelector('#close-modal').addEventListener('click', () => modal.remove());
		modal.querySelector('#cancel-btn').addEventListener('click', () => modal.remove());

		modal.querySelector('#add-apikey-form').addEventListener('submit', async (e) => {
			e.preventDefault();
			const formData = new FormData(e.target);
			const endpointNames = formData.getAll('endpoints');
			if (endpointNames.length === 0) {
				notifications.error(t('apikeys.selectEndpoints'));
				return;
			}

			const data = {
				name: formData.get('name'),
				endpointNames: endpointNames,
				enabled: formData.get('enabled') === 'on',
			};

			const expiresAt = formData.get('expiresAt');
			if (expiresAt) {
				data.expiresAt = new Date(expiresAt).toISOString();
			}

			try {
				await api.createAPIKey(data);
				notifications.success(t('apikeys.keyCreated'));
				modal.remove();
				await this.loadAPIKeys();
			} catch (error) {
				notifications.error(`${t('apikeys.failedToCreate')}: ${error.message}`);
			}
		});
	}

	async showEditModal(id) {
		const key = this.apiKeys.find(k => k.id === id);
		if (!key) return;

		await this.loadEndpoints();

		const modal = document.createElement('div');
		modal.className = 'modal-overlay apikeys-modal';
		modal.innerHTML = `
			<div class="modal">
				<div class="modal-header">
					<h3>${t('apikeys.editKey')}</h3>
					<button class="modal-close" id="close-modal">×</button>
				</div>
				<div class="modal-body">
					<form id="edit-apikey-form">
						<div class="form-group">
							<label class="form-label">${t('apikeys.keyName')} *</label>
							<input type="text" name="name" class="form-control" required value="${this.escapeHtml(key.name)}">
						</div>
						<div class="form-group">
							<label class="form-label">${t('apikeys.endpoints')} *</label>
							<div class="endpoint-checkboxes">
								${this.endpoints.map(ep => `
									<label class="checkbox-label">
										<input type="checkbox" name="endpoints" value="${this.escapeHtml(ep.name)}" ${key.endpointNames.includes(ep.name) ? 'checked' : ''}>
										${this.escapeHtml(ep.name)}
									</label>
								`).join('')}
							</div>
						</div>
						<div class="form-group">
							<label class="form-label">${t('apikeys.expiresAt')}</label>
							<input type="datetime-local" name="expiresAt" class="form-control" ${key.expiresAt ? `value="${new Date(key.expiresAt).toISOString().slice(0, 16)}"` : ''}>
							<small class="text-muted">${t('apikeys.never')}</small>
						</div>
						<div class="form-group">
							<label class="checkbox-label">
								<input type="checkbox" name="enabled" ${key.enabled ? 'checked' : ''}>
								${t('common.enabled')}
							</label>
						</div>
						<div class="modal-footer">
							<button type="button" class="btn btn-secondary" id="cancel-btn">${t('common.cancel')}</button>
							<button type="submit" class="btn btn-primary">${t('common.save')}</button>
						</div>
					</form>
				</div>
			</div>
		`;

		document.body.appendChild(modal);

		modal.querySelector('#close-modal').addEventListener('click', () => modal.remove());
		modal.querySelector('#cancel-btn').addEventListener('click', () => modal.remove());

		modal.querySelector('#edit-apikey-form').addEventListener('submit', async (e) => {
			e.preventDefault();
			const formData = new FormData(e.target);
			const endpointNames = formData.getAll('endpoints');
			if (endpointNames.length === 0) {
				notifications.error(t('apikeys.selectEndpoints'));
				return;
			}

			const data = {
				name: formData.get('name'),
				endpointNames: endpointNames,
				enabled: formData.get('enabled') === 'on',
			};

			const expiresAt = formData.get('expiresAt');
			if (expiresAt) {
				data.expiresAt = new Date(expiresAt).toISOString();
			}

			try {
				await api.updateAPIKey(id, data);
				notifications.success(t('apikeys.keyUpdated'));
				modal.remove();
				await this.loadAPIKeys();
			} catch (error) {
				notifications.error(`${t('apikeys.failedToUpdate')}: ${error.message}`);
			}
		});
	}

	async showConfigModal() {
		const modal = document.createElement('div');
		modal.className = 'modal-overlay apikeys-modal';
		modal.innerHTML = `
			<div class="modal">
				<div class="modal-header">
					<h3>⚙️ ${t('apikeys.authEnabled')}</h3>
					<button class="modal-close" id="close-modal">×</button>
				</div>
				<div class="modal-body">
					<div class="confirm-dialog">
						<div class="confirm-icon">${this.config.enabled ? '🔐' : '🔓'}</div>
						<h4>${t('apikeys.authEnabled')}</h4>
						<p>${t('apikeys.authEnabledHelp')}</p>
					</div>
					<div class="form-group mt-3">
						<label class="checkbox-label">
							<input type="checkbox" name="enabled" id="config-enabled-checkbox" ${this.config.enabled ? 'checked' : ''}>
							${t('apikeys.authEnabled')}
						</label>
					</div>
					<div class="modal-footer">
						<button type="button" class="btn btn-secondary" id="cancel-btn">${t('common.cancel')}</button>
						<button type="button" class="btn btn-primary" id="save-btn">${t('common.save')}</button>
					</div>
				</div>
			</div>
		`;

		document.body.appendChild(modal);

		modal.querySelector('#close-modal').addEventListener('click', () => modal.remove());
		modal.querySelector('#cancel-btn').addEventListener('click', () => modal.remove());

		modal.querySelector('#save-btn').addEventListener('click', async () => {
			const enabled = modal.querySelector('input[name="enabled"]').checked;

			try {
				await api.updateAPIKeysConfig({ enabled });
				this.config.enabled = enabled;
				this.updateAuthStatus();
				notifications.success(t('apikeys.configUpdated'));
				modal.remove();
			} catch (error) {
				notifications.error(`${t('apikeys.failedToUpdateConfig')}: ${error.message}`);
			}
		});
	}

	async loadEndpoints() {
		try {
			const data = await api.getEndpoints();
			this.endpoints = (data.endpoints || []).filter(ep => ep.enabled);
		} catch (error) {
			console.error('Failed to load endpoints:', error);
			notifications.error('Failed to load endpoints');
		}
	}

	async regenerateKey(id) {
		if (!confirm(t('apikeys.confirmRegenerate'))) return;

		try {
			await api.regenerateAPIKey(id);
			notifications.success(t('apikeys.keyRegenerated'));
			await this.loadAPIKeys();
		} catch (error) {
			notifications.error(`${t('apikeys.failedToRegenerate')}: ${error.message}`);
		}
	}

	async deleteKey(id) {
		const key = this.apiKeys.find(k => k.id === id);
		if (!key) return;

		if (!confirm(t('apikeys.confirmDelete').replace('{name}', key.name))) return;

		try {
			await api.deleteAPIKey(id);
			notifications.success(t('apikeys.keyDeleted'));
			await this.loadAPIKeys();
		} catch (error) {
			notifications.error(`${t('apikeys.failedToDelete')}: ${error.message}`);
		}
	}
}

export default new APIKeys();
