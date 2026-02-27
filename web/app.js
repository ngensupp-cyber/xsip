// ═══════════════════════════════════════════════════════
//  XSIP Carrier Platform — Dashboard Controller
// ═══════════════════════════════════════════════════════

const API = '/api';

// ─── Init ──────────────────────────────────────────────
document.addEventListener('DOMContentLoaded', () => {
    initCharts();
    loadAll();
    setInterval(loadAll, 4000);
});

function loadAll() {
    fetchStats();
    fetchCalls();
}

// ─── Navigation ────────────────────────────────────────
const pageTitles = {
    overview: ['System Overview', 'Real-time carrier network monitoring'],
    subscribers: ['Subscribers', 'Manage subscriber accounts and billing'],
    calls: ['Live Calls', 'Active call sessions across the network'],
    cdr: ['Call Records', 'Historical call detail records'],
    security: ['Security', 'Firewall rules and threat protection'],
    network: ['Network', 'Topology, protocols and client setup'],
    settings: ['Settings', 'System configuration parameters'],
};

function navigate(pageId, el) {
    document.querySelectorAll('.page').forEach(p => p.classList.remove('active'));
    document.querySelectorAll('.nav-item').forEach(n => n.classList.remove('active'));

    const page = document.getElementById('page-' + pageId);
    if (page) {
        page.classList.remove('active');
        // Force reflow for animation
        void page.offsetWidth;
        page.classList.add('active');
    }
    if (el) el.classList.add('active');

    const info = pageTitles[pageId] || ['', ''];
    document.getElementById('page-title').textContent = info[0];
    document.getElementById('page-subtitle').textContent = info[1];

    // Load data for specific pages
    if (pageId === 'subscribers') fetchUsers();
    if (pageId === 'calls') fetchCalls();
    if (pageId === 'settings') fetchConfig();
}

// ─── Stats ─────────────────────────────────────────────
function fetchStats() {
    fetch(API + '/stats')
        .then(r => r.json())
        .then(d => {
            setText('kpi-calls', d.active_calls || 0);
            setText('kpi-users', d.total_users || 0);
            setText('kpi-status', d.system_status === 'operational' ? 'Healthy' : d.system_status);
            setText('kpi-version', d.version || '—');
        })
        .catch(() => { });
}

// ─── Users CRUD ────────────────────────────────────────
function fetchUsers() {
    fetch(API + '/users')
        .then(r => r.json())
        .then(users => {
            const tb = document.getElementById('sub-tbody');
            if (!users || users.length === 0) {
                tb.innerHTML = '<tr><td colspan="6" class="empty-state">No subscribers yet. Add the first one to get started.</td></tr>';
                setText('sub-count', 0);
                return;
            }
            setText('sub-count', users.length);
            tb.innerHTML = users.map(u => {
                const tier = u.level === 2 ? '<span class="tier tier-admin">Admin</span>' :
                    u.level === 1 ? '<span class="tier tier-reseller">Reseller</span>' :
                        '<span class="tier tier-user">User</span>';
                const bal = (u.balance || 0).toFixed(2);
                const balColor = u.balance > 0 ? 'var(--green)' : 'var(--red)';
                return `<tr>
                    <td><strong>${esc(u.username || u.id)}</strong></td>
                    <td>${esc(u.id)}</td>
                    <td style="font-family:monospace;font-size:0.78rem">sip:${esc(u.id)}@server</td>
                    <td style="color:${balColor};font-weight:600">$${bal}</td>
                    <td>${tier}</td>
                    <td>
                        <button class="btn-sm" onclick="openBalance('${esc(u.id)}',${u.balance || 0})">Balance</button>
                        <button class="btn-sm danger" onclick="deleteSub('${esc(u.id)}')">Delete</button>
                    </td>
                </tr>`;
            }).join('');
        })
        .catch(() => { });
}

function submitNewSub(e) {
    e.preventDefault();
    const data = {
        id: document.getElementById('f-id').value.trim(),
        username: document.getElementById('f-name').value.trim(),
        password: document.getElementById('f-pass').value,
        balance: parseFloat(document.getElementById('f-bal').value) || 0,
        tenant_id: 'default',
        level: 0
    };
    fetch(API + '/users', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data)
    }).then(r => {
        if (r.ok) {
            closeModal('add-sub');
            document.getElementById('form-add-sub').reset();
            fetchUsers();
            fetchStats();
            logActivity('Subscriber created: ' + data.username + ' (' + data.id + ')');
        }
    });
}

function deleteSub(id) {
    if (!confirm('Delete subscriber ' + id + '?')) return;
    fetch(API + '/users/' + id, { method: 'DELETE' })
        .then(() => {
            fetchUsers();
            fetchStats();
            logActivity('Subscriber removed: ' + id);
        });
}

function openBalance(id, current) {
    document.getElementById('eb-id').value = id;
    document.getElementById('eb-amount').value = current;
    openModal('edit-bal');
}

function submitBalance(e) {
    e.preventDefault();
    const id = document.getElementById('eb-id').value;
    const amount = parseFloat(document.getElementById('eb-amount').value);
    fetch(API + '/users/' + id + '/balance', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ amount })
    }).then(() => {
        closeModal('edit-bal');
        fetchUsers();
        logActivity('Balance updated for ' + id + ': $' + amount.toFixed(2));
    });
}

// ─── Calls ─────────────────────────────────────────────
function fetchCalls() {
    fetch(API + '/calls/active')
        .then(r => r.json())
        .then(calls => {
            const tb = document.getElementById('call-tbody');
            if (!calls || calls.length === 0) {
                tb.innerHTML = '<tr><td colspan="6" class="empty-state">No active calls at this time</td></tr>';
                setText('call-count', 0);
                return;
            }
            setText('call-count', calls.length);
            tb.innerHTML = calls.map(c => {
                const dur = Math.floor((Date.now() - new Date(c.start_time).getTime()) / 1000);
                const mm = Math.floor(dur / 60);
                const ss = dur % 60;
                return `<tr>
                    <td style="font-family:monospace;font-size:0.78rem">${esc(c.from)}</td>
                    <td style="font-family:monospace;font-size:0.78rem">${esc(c.to)}</td>
                    <td><span class="tier tier-admin">${esc(c.state)}</span></td>
                    <td>${mm}:${String(ss).padStart(2, '0')}</td>
                    <td>$${(c.rate || 0.01).toFixed(3)}</td>
                    <td>${esc(c.tenant_id || 'default')}</td>
                </tr>`;
            }).join('');
        })
        .catch(() => { });
}

// ─── Config ────────────────────────────────────────────
function fetchConfig() {
    fetch(API + '/config')
        .then(r => r.json())
        .then(cfg => {
            setText('cfg-proto', cfg.sip_protocol || 'TCP');
            setText('cfg-max', (cfg.max_concurrent_calls || 100000).toLocaleString());
            setText('cfg-rate', '$' + (cfg.billing_rate || 0.01));
            setText('cfg-ttl', cfg.registration_ttl || '1h');
            setText('cfg-fw', (cfg.firewall_threshold || 5) + ' attempts');
            setText('net-proto', cfg.sip_protocol || 'TCP');
            setText('net-cap', ((cfg.max_concurrent_calls || 100000) / 1000) + 'K');
        })
        .catch(() => { });
}

// ─── Modals ────────────────────────────────────────────
function openModal(name) {
    const el = document.getElementById('modal-' + name);
    if (el) el.classList.add('open');
}

function closeModal(name) {
    const el = document.getElementById('modal-' + name);
    if (el) el.classList.remove('open');
}

document.querySelectorAll('.overlay').forEach(ov => {
    ov.addEventListener('click', e => {
        if (e.target === ov) ov.classList.remove('open');
    });
});

// ─── Activity Log ──────────────────────────────────────
function logActivity(msg) {
    const list = document.getElementById('activity-log');
    if (!list) return;
    const li = document.createElement('li');
    const now = new Date();
    li.textContent = now.toLocaleTimeString() + ' — ' + msg;
    list.prepend(li);
    while (list.children.length > 20) list.removeChild(list.lastChild);
}

// ─── Charts ────────────────────────────────────────────
function initCharts() {
    // Throughput Line Chart
    const ctx1 = document.getElementById('throughputChart');
    if (ctx1) {
        new Chart(ctx1.getContext('2d'), {
            type: 'line',
            data: {
                labels: Array.from({ length: 15 }, (_, i) => i * 4 + 's'),
                datasets: [{
                    label: 'Packets/s',
                    data: [42, 58, 45, 67, 52, 71, 63, 80, 72, 91, 85, 102, 95, 110, 98],
                    borderColor: '#9c8b72',
                    backgroundColor: 'rgba(184,167,134,0.08)',
                    fill: true,
                    tension: 0.3,
                    borderWidth: 1.5,
                    pointRadius: 0,
                }]
            },
            options: chartOpts()
        });
    }

    // Call Distribution Bar Chart
    const ctx2 = document.getElementById('callDistChart');
    if (ctx2) {
        new Chart(ctx2.getContext('2d'), {
            type: 'bar',
            data: {
                labels: ['00:00', '04:00', '08:00', '12:00', '16:00', '20:00'],
                datasets: [{
                    label: 'Calls',
                    data: [12, 5, 45, 78, 62, 34],
                    backgroundColor: 'rgba(184,167,134,0.3)',
                    borderColor: '#9c8b72',
                    borderWidth: 1,
                    borderRadius: 4,
                }]
            },
            options: chartOpts()
        });
    }
}

function chartOpts() {
    return {
        responsive: true,
        maintainAspectRatio: false,
        plugins: { legend: { display: false } },
        scales: {
            y: {
                grid: { color: 'rgba(0,0,0,0.04)', drawBorder: false },
                ticks: { color: '#a89a85', font: { size: 10 } },
                border: { display: false }
            },
            x: {
                grid: { display: false },
                ticks: { color: '#a89a85', font: { size: 10 } },
                border: { display: false }
            }
        }
    };
}

// ─── Helpers ───────────────────────────────────────────
function setText(id, val) {
    const el = document.getElementById(id);
    if (el) el.textContent = val;
}

function esc(s) {
    if (!s) return '';
    const d = document.createElement('div');
    d.textContent = s;
    return d.innerHTML;
}
