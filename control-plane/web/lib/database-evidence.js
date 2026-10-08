// Shared by the rule explanation and the incident's evidence graphs.
export const databaseEvidence = {
    'db-up': ['db-up', 'db-probe'],
    'db-probe': ['db-probe', 'db-active', 'db-connection-usage', 'db-lock-waiters'],
    'db-connection-usage': ['db-connection-usage', 'db-connections', 'db-active', 'db-probe'],
    'db-lock-waiters': ['db-lock-waiters', 'db-lock-waits', 'db-active', 'db-probe'],
    'db-slow-queries': ['db-slow-queries', 'db-statements', 'db-probe'],
    'db-monitoring-ready': ['db-monitoring-ready', 'db-up', 'db-probe'],
};
