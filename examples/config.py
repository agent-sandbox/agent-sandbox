import os


def init_config():
    os.environ['E2B_API_URL'] = 'http://localhost:10000/e2b/v1'
    os.environ['E2B_API_KEY'] = 'testuser-aef134ef-7aa1-945e-9399-7df9a4ad0c3f'
    os.environ['E2B_DOMAIN'] = 'localhost:10000'


    def _connection_get_sandbox_url(self, sandbox_id: str, sandbox_domain: str) -> str:
        return f"http://{self.get_host(sandbox_id, sandbox_domain, self.envd_port)}"


    def __connection_config_get_host(_, sandbox_id: str, sandbox_domain: str, port: int) -> str:
        return f"{sandbox_domain}/sandboxes/router/{sandbox_id}/{port}"


    from e2b import ConnectionConfig

    ConnectionConfig.get_host = __connection_config_get_host
    ConnectionConfig.get_sandbox_url = _connection_get_sandbox_url