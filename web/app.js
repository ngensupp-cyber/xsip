document.addEventListener('DOMContentLoaded', () => {
    initChart();
    fetchStats();
    fetchUsers();
    
    // Auto-refresh stats every 3 seconds
    setInterval(fetchStats, 3000);
});

function initChart() {
    const ctx = document.getElementById('networkChart').getContext('2d');
    new Chart(ctx, {
        type: 'line',
        data: {
            labels: ['10:00', '10:05', '10:10', '10:15', '10:20', '10:25'],
            datasets: [{
                label: 'Packets/sec',
                data: [120, 190, 80, 150, 220, 300],
                borderColor: '#58a6ff',
                backgroundColor: 'rgba(88, 166, 255, 0.1)',
                fill: true,
                tension: 0.4
            }]
        },
        options: {
            plugins: { legend: { display: false } },
            scales: { y: { grid: { color: '#30363d' } }, x: { grid: { color: '#30363d' } } }
        }
    });
}

function fetchStats() {
    fetch('/stats')
        .then(res => res.json())
        .then(data => {
            document.getElementById('stat-active-calls').innerText = data.active_calls;
            document.getElementById('stat-registrations').innerText = data.registrations || 0;
        })
        .catch(err => console.error('Error fetching stats:', err));
}

function fetchUsers() {
    fetch('/users')
        .then(res => res.json())
        .then(users => {
            const body = document.getElementById('user-table-body');
            body.innerHTML = '';
            users.forEach(user => {
                body.innerHTML += `
                    <tr>
                        <td>${user.username}</td>
                        <td>sip:${user.id}@domain</td>
                        <td>$${user.balance.toFixed(2)}</td>
                        <td><span class="status-dot online"></span> نشط</td>
                        <td>
                            <button class="btn-edit">تعديل</button>
                            <button class="btn-delete" onclick="deleteUser('${user.id}')">حذف</button>
                        </td>
                    </tr>
                `;
            });
        });
}

function showModal(id) { document.getElementById(id).style.display = 'flex'; }
function hideModal(id) { document.getElementById(id).style.display = 'none'; }

// Form Submission
document.getElementById('user-form').addEventListener('submit', (e) => {
    e.preventDefault();
    const data = {
        id: document.getElementById('user-sip').value,
        username: document.getElementById('user-name').value,
        password: document.getElementById('user-pass').value,
        balance: parseFloat(document.getElementById('user-balance').value),
        tenant_id: 'default'
    };

    fetch('/users', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data)
    }).then(() => {
        hideModal('user-modal');
        fetchUsers();
    });
});
