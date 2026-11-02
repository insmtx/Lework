"""Check every built-in workload template before touching a shared Worker configuration."""

CONFIG_NAME = 'leros-worker-config'
RESOURCE_KINDS = 'deployments,statefulsets,daemonsets,jobs,cronjobs,replicasets,replicationcontrollers,pods'


def owner_uid(item, kind):
    owners = item.get('metadata', {}).get('ownerReferences', [])
    return next((o.get('uid') for o in owners if o.get('kind') == kind and o.get('controller') is True), None)


def pod_spec(item):
    spec = item.get('spec', {})
    if item.get('kind') == 'Pod':
        return spec
    if item.get('kind') == 'CronJob':
        spec = spec.get('jobTemplate', {}).get('spec', {})
    return spec.get('template', {}).get('spec', {})


def uses_config(spec):
    for volume in spec.get('volumes', []):
        if volume.get('configMap', {}).get('name') == CONFIG_NAME:
            return True
        for source in volume.get('projected', {}).get('sources', []):
            if source.get('configMap', {}).get('name') == CONFIG_NAME:
                return True
    for container in spec.get('containers', []) + spec.get('initContainers', []):
        for source in container.get('envFrom', []):
            if source.get('configMapRef', {}).get('name') == CONFIG_NAME:
                return True
        for value in container.get('env', []):
            if value.get('valueFrom', {}).get('configMapKeyRef', {}).get('name') == CONFIG_NAME:
                return True
    return False


def validate_consumers(items, worker_uid):
    if not worker_uid:
        raise ValueError('target Worker UID missing')
    replica_sets = {item.get('metadata', {}).get('uid') for item in items
                    if item.get('kind') == 'ReplicaSet' and owner_uid(item, 'Deployment') == worker_uid}
    replica_sets.discard(None)
    for item in items:
        spec = pod_spec(item)
        if not uses_config(spec):
            continue
        kind = item.get('kind')
        metadata = item.get('metadata', {})
        allowed = (kind == 'Deployment' and metadata.get('uid') == worker_uid
                   or kind == 'ReplicaSet' and owner_uid(item, 'Deployment') == worker_uid
                   or kind == 'Pod' and owner_uid(item, 'ReplicaSet') in replica_sets
                   and spec.get('nodeName') in (None, 'master'))
        if not allowed:
            raise ValueError('shared Worker config has another consumer; abort and prepare an isolated deployment')
