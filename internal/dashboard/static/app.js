// GoQueue Dashboard - Real-time monitoring via Server-Sent Events

(function () {
    'use strict';

    let eventSource = null;
    let currentQueue = '';
    let queues = {};
    let allJobs = [];

    // --- SSE Connection ---
    function connect() {
        if (eventSource) eventSource.close();

        eventSource = new EventSource('/api/events');

        eventSource.addEventListener('stats', function (e) {
            const data = JSON.parse(e.data);
            updateMetrics(data.metrics);
            updateQueues(data.queues);
            updateWorkers(data.workers);
        });

        eventSource.onopen = function () {
            document.getElementById('connection-dot').className = 'w-2 h-2 rounded-full bg-emerald-500';
            document.getElementById('connection-status').textContent = 'Connected';
            document.getElementById('connection-status').className = 'connected text-sm';
        };

        eventSource.onerror = function () {
            document.getElementById('connection-dot').className = 'w-2 h-2 rounded-full bg-red-500 pulse';
            document.getElementById('connection-status').textContent = 'Disconnected';
            document.getElementById('connection-status').className = 'disconnected text-sm';
        };
    }

    // --- Update Functions ---
    function updateMetrics(m) {
        setText('stat-processed', formatNum(m.jobs_processed));
        setText('stat-succeeded', formatNum(m.jobs_succeeded));
        setText('stat-failed', formatNum(m.jobs_failed));
        setText('stat-enqueued', formatNum(m.jobs_enqueued));
        setText('stat-retried', formatNum(m.jobs_retried));
        setText('stat-dead', formatNum(m.jobs_dead));
        setText('stat-throughput', m.throughput.toFixed(1));
    }

    function updateQueues(queueData) {
        queues = queueData || {};
        renderQueueOverview();
        renderQueueTabs();
        fetchJobs();
    }

    function updateWorkers(workers) {
        // Workers info is included in queue overview
    }

    function renderQueueOverview() {
        const container = document.getElementById('queue-overview');
        const names = Object.keys(queues);

        if (names.length === 0) {
            container.innerHTML = '<p class="text-gray-500 text-sm">No queues active</p>';
            return;
        }

        container.innerHTML = names.map(function (name) {
            const q = queues[name];
            const total = q.pending + q.processing + q.completed + q.failed + q.dead;
            const maxBar = Math.max(total, 1);

            return '<div class="mb-4 last:mb-0">' +
                '<div class="flex items-center justify-between mb-1">' +
                '<span class="text-sm font-medium text-white">' + name + '</span>' +
                '<div class="flex gap-3 text-xs text-gray-400">' +
                '<span>Pending: <span class="text-blue-400">' + q.pending + '</span></span>' +
                '<span>Processing: <span class="text-emerald-400">' + q.processing + '</span></span>' +
                '<span>Completed: <span class="text-teal-400">' + q.completed + '</span></span>' +
                '<span>Failed: <span class="text-red-400">' + q.failed + '</span></span>' +
                '<span>Dead: <span class="text-purple-400">' + q.dead + '</span></span>' +
                '</div></div>' +
                '<div class="flex h-2 rounded-full overflow-hidden bg-gray-700">' +
                barSegment(q.completed, maxBar, '#2dd4bf') +
                barSegment(q.processing, maxBar, '#34d399') +
                barSegment(q.pending, maxBar, '#60a5fa') +
                barSegment(q.failed, maxBar, '#f87171') +
                barSegment(q.dead, maxBar, '#c084fc') +
                '</div></div>';
        }).join('');
    }

    function barSegment(value, total, color) {
        if (value <= 0) return '';
        var pct = (value / total * 100).toFixed(1);
        return '<div class="bar" style="width:' + pct + '%;background:' + color + '"></div>';
    }

    function renderQueueTabs() {
        const container = document.getElementById('queue-tabs');
        const names = ['all'].concat(Object.keys(queues));

        container.innerHTML = names.map(function (name) {
            const active = (name === 'all' && !currentQueue) || name === currentQueue;
            return '<button class="tab text-xs ' + (active ? 'active' : 'text-gray-400') + '" onclick="window.GoQueue.selectQueue(\'' + name + '\')">' + name + '</button>';
        }).join('');
    }

    function fetchJobs() {
        var url = currentQueue ? '/api/queues/' + currentQueue + '/jobs' : '/api/queues/default/jobs';

        fetch(url)
            .then(function (r) { return r.json(); })
            .then(function (jobs) {
                allJobs = jobs || [];
                renderJobs();
            })
            .catch(function () { });
    }

    function renderJobs() {
        const tbody = document.getElementById('jobs-body');

        if (!allJobs || allJobs.length === 0) {
            tbody.innerHTML = '<tr><td colspan="7" class="px-4 py-8 text-center text-gray-500">No jobs found</td></tr>';
            return;
        }

        tbody.innerHTML = allJobs.slice(0, 50).map(function (j) {
            var actions = '';
            if (j.status === 'failed' || j.status === 'dead') {
                actions = '<button class="btn btn-retry" onclick="window.GoQueue.retryJob(\'' + j.id + '\')">Retry</button> ' +
                    '<button class="btn btn-delete" onclick="window.GoQueue.deleteJob(\'' + j.id + '\')">Delete</button>';
            }

            return '<tr class="border-b border-gray-800 hover:bg-gray-800/50">' +
                '<td class="px-4 py-2 font-mono text-xs text-gray-400">' + j.id.substring(0, 12) + '...</td>' +
                '<td class="px-4 py-2">' + j.type + '</td>' +
                '<td class="px-4 py-2 text-gray-400">' + j.queue + '</td>' +
                '<td class="px-4 py-2"><span class="badge badge-' + j.status + '">' + j.status + '</span></td>' +
                '<td class="px-4 py-2 text-gray-400">' + j.attempts + '/' + j.max_retries + '</td>' +
                '<td class="px-4 py-2 text-gray-400 text-xs">' + timeAgo(j.created_at) + '</td>' +
                '<td class="px-4 py-2">' + actions + '</td>' +
                '</tr>';
        }).join('');
    }

    // --- Actions ---
    function retryJob(id) {
        fetch('/api/jobs/' + id + '/retry', { method: 'POST' })
            .then(function () { fetchJobs(); });
    }

    function deleteJob(id) {
        fetch('/api/jobs/' + id, { method: 'DELETE' })
            .then(function () { fetchJobs(); });
    }

    function selectQueue(name) {
        currentQueue = name === 'all' ? '' : name;
        renderQueueTabs();
        fetchJobs();
    }

    // --- Helpers ---
    function setText(id, text) {
        var el = document.getElementById(id);
        if (el) el.textContent = text;
    }

    function formatNum(n) {
        if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M';
        if (n >= 1000) return (n / 1000).toFixed(1) + 'K';
        return String(n);
    }

    function timeAgo(dateStr) {
        if (!dateStr) return '-';
        var diff = (Date.now() - new Date(dateStr).getTime()) / 1000;
        if (diff < 60) return Math.floor(diff) + 's ago';
        if (diff < 3600) return Math.floor(diff / 60) + 'm ago';
        if (diff < 86400) return Math.floor(diff / 3600) + 'h ago';
        return Math.floor(diff / 86400) + 'd ago';
    }

    // Expose to global scope for onclick handlers
    window.GoQueue = {
        retryJob: retryJob,
        deleteJob: deleteJob,
        selectQueue: selectQueue
    };

    // Start
    connect();
    // Fetch initial data
    fetch('/api/stats').then(function (r) { return r.json(); }).then(function (data) {
        if (data.metrics) updateMetrics(data.metrics);
        if (data.queues) updateQueues(data.queues);
    }).catch(function () { });
})();
