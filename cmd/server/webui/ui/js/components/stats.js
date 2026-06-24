import { api } from '../api.js';
import { notifications } from '../utils/notifications.js';
import { formatNumber, formatTokens } from '../utils/formatters.js';
import { t } from '../utils/i18n.js';

class Stats {
    constructor() {
        this.container = document.getElementById('view-container');
        /* API Key 用量表格的排序状态：默认输入令牌降序 */
        this.apiKeySortState = { column: 'inputTokens', direction: 'desc' };
        // 监听语言切换
        window.addEventListener('languageChanged', () => {
            if (state.get('currentView') === 'stats') {
                this.render();
            }
        });
    }

    /* 按当前排序状态对 API Key 列表排序，返回新数组 */
    sortAPIKeys(keys) {
        const { column, direction } = this.apiKeySortState;
        const sorted = [...keys].sort((a, b) => {
            const va = a[column] || 0;
            const vb = b[column] || 0;
            return va - vb;
        });
        if (direction === 'desc') {
            sorted.reverse();
        }
        return sorted;
    }

    async render() {
        this.container.innerHTML = `
            <div class="stats">
                <h1>${t('stats.title')}</h1>

                <div class="flex gap-2 mt-3 mb-3">
                    <button class="btn btn-sm btn-primary period-btn active" data-period="daily">${t('stats.daily')}</button>
                    <button class="btn btn-sm btn-secondary period-btn" data-period="weekly">${t('stats.weekly')}</button>
                    <button class="btn btn-sm btn-secondary period-btn" data-period="monthly">${t('stats.monthly')}</button>
                    <button class="btn btn-sm btn-secondary period-btn" data-type="apikeys">${t('stats.apiKeyBreakdown')}</button>
                </div>

                <div id="stats-content"></div>
            </div>
        `;

        document.querySelectorAll('.period-btn').forEach(btn => {
            btn.addEventListener('click', () => {
                document.querySelectorAll('.period-btn').forEach(b => {
                    b.classList.remove('btn-primary', 'active');
                    b.classList.add('btn-secondary');
                });
                btn.classList.remove('btn-secondary');
                btn.classList.add('btn-primary', 'active');

                if (btn.dataset.type === 'apikeys') {
                    this.loadAPIKeyStats();
                } else {
                    this.loadStats(btn.dataset.period);
                }
            });
        });

        await this.loadStats('daily');
    }

    async loadAPIKeyStats() {
        const container = document.getElementById('stats-content');
        container.innerHTML = `<div class="empty-state"><p>${t('common.loading') || 'Loading...'}...</p></div>`;
        try {
            const data = await api.getAPIKeysStatsSummary();
            this.renderAPIKeyStats(data.keys || []);
        } catch (error) {
            container.innerHTML = `<div class="empty-state"><p>${t('stats.failedToLoad')}: ${error.message}</p></div>`;
        }
    }

    renderAPIKeyStats(keys) {
        const container = document.getElementById('stats-content');

        let totalRequests = 0, totalErrors = 0;
        let totalInputTokens = 0, totalOutputTokens = 0;
        keys.forEach(k => {
            totalRequests += k.requests || 0;
            totalErrors += k.errors || 0;
            totalInputTokens += k.inputTokens || 0;
            totalOutputTokens += k.outputTokens || 0;
        });

        container.innerHTML = `
            <div class="grid grid-cols-6 mb-4">
                <div class="stat-card">
                    <div class="stat-label">${t('stats.totalRequests')}</div>
                    <div class="stat-value">${formatNumber(totalRequests)}</div>
                </div>
                <div class="stat-card">
                    <div class="stat-label">${t('stats.successful')}</div>
                    <div class="stat-value">${formatNumber(totalRequests - totalErrors)}</div>
                </div>
                <div class="stat-card">
                    <div class="stat-label">${t('stats.errors')}</div>
                    <div class="stat-value">${formatNumber(totalErrors)}</div>
                </div>
                <div class="stat-card">
                    <div class="stat-label">${t('stats.totalTokens')}</div>
                    <div class="stat-value">${formatTokens(totalInputTokens + totalOutputTokens)}</div>
                </div>
                <div class="stat-card">
                    <div class="stat-label">${t('stats.inputTokens')}</div>
                    <div class="stat-value">${formatTokens(totalInputTokens)}</div>
                </div>
                <div class="stat-card">
                    <div class="stat-label">${t('stats.outputTokens')}</div>
                    <div class="stat-value">${formatTokens(totalOutputTokens)}</div>
                </div>
            </div>

            <div class="card">
                <div class="card-header card-header-row">
                    <h3 class="card-title">${t('stats.apiKeyBreakdown')}</h3>
                    <button class="btn btn-sm btn-secondary" id="refresh-apikey-stats-btn">
                        ${t('common.refresh')}
                    </button>
                </div>
                <div class="card-body" id="apikey-stats-table">
                    ${this.renderAPIKeyTable(keys)}
                </div>
            </div>
        `;

        document.getElementById('refresh-apikey-stats-btn').addEventListener('click', () => {
            this.loadAPIKeyStats();
        });

        const scope = document.getElementById('apikey-stats-table');
        if (scope) {
            this.attachAPIKeySortHandlers(scope, keys);
        }
    }

    renderAPIKeyTable(keys) {
        const sorted = this.sortAPIKeys(keys);
        if (sorted.length === 0) {
            return `<div class="empty-state"><p>${t('stats.noDataAvailable')}</p></div>`;
        }

        const { column, direction } = this.apiKeySortState;
        const thClass = (col) => column === col ? 'sortable sorted' : 'sortable';
        const indicator = (col) => column === col ? (direction === 'desc' ? ' ▾' : ' ▴') : '';

        return `
            <div class="table-container">
                <table class="table">
                    <thead>
                        <tr>
                            <th>#</th>
                            <th>${t('stats.apiKey')}</th>
                            <th>${t('stats.requests')}</th>
                            <th>${t('stats.errors')}</th>
                            <th class="${thClass('inputTokens')}" data-sort="inputTokens">${t('stats.inputTokens')}${indicator('inputTokens')}</th>
                            <th class="${thClass('outputTokens')}" data-sort="outputTokens">${t('stats.outputTokens')}${indicator('outputTokens')}</th>
                            <th>${t('stats.lastUsedAt')}</th>
                        </tr>
                    </thead>
                    <tbody>
                        ${sorted.map((k, i) => `
                            <tr>
                                <td>${i + 1}</td>
                                <td><strong>${this.escapeHtml(k.name)}</strong></td>
                                <td>${formatNumber(k.requests || 0)}</td>
                                <td>${formatNumber(k.errors || 0)}</td>
                                <td>${formatTokens(k.inputTokens || 0)}</td>
                                <td>${formatTokens(k.outputTokens || 0)}</td>
                                <td>${k.lastUsedAt ? new Date(k.lastUsedAt).toLocaleString() : '—'}</td>
                            </tr>
                        `).join('')}
                    </tbody>
                </table>
            </div>
        `;
    }

    /* 绑定 API Key 表头排序点击事件；scope 为包含表格的容器，keys 为已缓存数据 */
    attachAPIKeySortHandlers(scope, keys) {
        const headers = scope.querySelectorAll('.sortable[data-sort]');
        headers.forEach(th => {
            th.addEventListener('click', () => {
                const col = th.dataset.sort;
                const { column, direction } = this.apiKeySortState;
                if (column === col) {
                    this.apiKeySortState.direction = direction === 'desc' ? 'asc' : 'desc';
                } else {
                    this.apiKeySortState.column = col;
                    this.apiKeySortState.direction = 'desc';
                }
                this.refreshAPIKeyTable(scope, keys);
            });
        });
    }

    /* 用缓存的 keys 重新渲染容器内的表格并重新绑定排序事件 */
    refreshAPIKeyTable(scope, keys) {
        const tableContainer = scope.querySelector('.table-container');
        if (tableContainer) {
            tableContainer.outerHTML = this.renderAPIKeyTable(keys);
            this.attachAPIKeySortHandlers(scope, keys);
        }
    }

    async loadStats(period) {
        try {
            let data;
            switch (period) {
                case 'daily':
                    data = await api.getStatsDaily();
                    break;
                case 'weekly':
                    data = await api.getStatsWeekly();
                    break;
                case 'monthly':
                    data = await api.getStatsMonthly();
                    break;
            }

            this.renderStats(data);

            /* 加载同周期的 API Key 用量 */
            if (data) {
                var start = data.startDate || data.date;
                var end = data.endDate || data.date;
                this.loadAPIKeyPeriodStats(start, end);
            }
        } catch (error) {
            notifications.error(`${t('stats.failedToLoad')}: ${error.message}`);
        }
    }

    async loadAPIKeyPeriodStats(start, end) {
        var container = document.getElementById('apikey-period-stats');
        if (!container) {
            return;
        }

        try {
            var data = await api.getAPIKeysStatsPeriod(start, end);
            this.renderAPIKeyPeriodStats(data, container);
            this.attachAPIKeySortHandlers(container, data.keys || []);
        } catch (error) {
            container.innerHTML = '';
        }
    }

    renderAPIKeyPeriodStats(data, container) {
        var keys = data.keys || [];
        var totalRequests = data.totalRequests || 0;
        var totalErrors = data.totalErrors || 0;
        var totalInputTokens = data.totalInputTokens || 0;
        var totalOutputTokens = data.totalOutputTokens || 0;
        var totalTokens = totalInputTokens + totalOutputTokens;

        container.innerHTML = `
            <div class="card">
                <div class="card-header">
                    <h3 class="card-title">🔑 ${t('stats.apiKeyBreakdown')}</h3>
                </div>
                <div class="card-body">
                    <div class="grid grid-cols-6 mb-3">
                        <div class="stat-card">
                            <div class="stat-label">${t('stats.totalRequests')}</div>
                            <div class="stat-value">${formatNumber(totalRequests)}</div>
                        </div>
                        <div class="stat-card">
                            <div class="stat-label">${t('stats.successful')}</div>
                            <div class="stat-value">${formatNumber(totalRequests - totalErrors)}</div>
                        </div>
                        <div class="stat-card">
                            <div class="stat-label">${t('stats.errors')}</div>
                            <div class="stat-value">${formatNumber(totalErrors)}</div>
                        </div>
                        <div class="stat-card">
                            <div class="stat-label">${t('stats.totalTokens')}</div>
                            <div class="stat-value">${formatTokens(totalTokens)}</div>
                        </div>
                        <div class="stat-card">
                            <div class="stat-label">${t('stats.inputTokens')}</div>
                            <div class="stat-value">${formatTokens(totalInputTokens)}</div>
                        </div>
                        <div class="stat-card">
                            <div class="stat-label">${t('stats.outputTokens')}</div>
                            <div class="stat-value">${formatTokens(totalOutputTokens)}</div>
                        </div>
                    </div>
                    ${this.renderAPIKeyTable(keys)}
                </div>
            </div>
        `;
    }

    renderStats(data) {
        const stats = data.stats || {};
        const container = document.getElementById('stats-content');

        container.innerHTML = `
            <div class="grid grid-cols-6 mb-4">
                <div class="stat-card">
                    <div class="stat-label">${t('stats.totalRequests')}</div>
                    <div class="stat-value">${formatNumber(stats.totalRequests || 0)}</div>
                </div>
                <div class="stat-card">
                    <div class="stat-label">${t('stats.successful')}</div>
                    <div class="stat-value">${formatNumber(stats.totalSuccess || 0)}</div>
                </div>
                <div class="stat-card">
                    <div class="stat-label">${t('stats.errors')}</div>
                    <div class="stat-value">${formatNumber(stats.totalErrors || 0)}</div>
                </div>
                <div class="stat-card">
                    <div class="stat-label">${t('stats.totalTokens')}</div>
                    <div class="stat-value">${formatTokens((stats.totalInputTokens || 0) + (stats.totalOutputTokens || 0))}</div>
                </div>
                <div class="stat-card">
                    <div class="stat-label">${t('stats.inputTokens')}</div>
                    <div class="stat-value">${formatTokens(stats.totalInputTokens || 0)}</div>
                </div>
                <div class="stat-card">
                    <div class="stat-label">${t('stats.outputTokens')}</div>
                    <div class="stat-value">${formatTokens(stats.totalOutputTokens || 0)}</div>
                </div>
            </div>

            <div class="card">
                <div class="card-header">
                    <h3 class="card-title">${t('stats.endpointBreakdown')}</h3>
                </div>
                <div class="card-body">
                    ${this.renderEndpointTable(stats.endpoints || {})}
                </div>
            </div>

            <div id="apikey-period-stats" style="margin-top: 24px;"></div>
        `;
    }

    renderEndpointTable(endpoints) {
        const endpointNames = Object.keys(endpoints);

        if (endpointNames.length === 0) {
            return `<div class="empty-state"><p>${t('stats.noDataAvailable')}</p></div>`;
        }

        return `
            <div class="table-container">
                <table class="table">
                    <thead>
                        <tr>
                            <th>${t('stats.endpoint')}</th>
                            <th>${t('stats.requests')}</th>
                            <th>${t('stats.errors')}</th>
                            <th>${t('stats.inputTokens')}</th>
                            <th>${t('stats.outputTokens')}</th>
                        </tr>
                    </thead>
                    <tbody>
                        ${endpointNames.map(name => {
                            const ep = endpoints[name];
                            return `
                                <tr>
                                    <td><strong>${this.escapeHtml(name)}</strong></td>
                                    <td>${formatNumber(ep.requests || 0)}</td>
                                    <td>${formatNumber(ep.errors || 0)}</td>
                                    <td>${formatTokens(ep.inputTokens || 0)}</td>
                                    <td>${formatTokens(ep.outputTokens || 0)}</td>
                                </tr>
                            `;
                        }).join('')}
                    </tbody>
                </table>
            </div>
        `;
    }

    escapeHtml(text) {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }
}

export const stats = new Stats();
