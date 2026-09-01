import os

# for no https and forward to sandbox by path vars
def dev():
    os.environ['E2B_API_URL'] = 'http://example.domain.com/e2b/v1'
    os.environ['E2B_DOMAIN'] = 'example.domain.com'
    os.environ['E2B_API_KEY'] = 'testuser-aef134ef-7aa1-945e-9399-7df9a4ad0c3f'

    # use path to forward to sandbox, instead of subdomain
    def __connection_config_get_host(_, sandbox_id: str, sandbox_domain: str, port: int) -> str:
        #return f"{port}-{sandbox_id}.{sandbox_domain}"
        return f"{sandbox_domain}/sandboxes/router/{sandbox_id}/{port}"

     # use http instead of https for sandbox url
    def __get_sandbox_url(self, sandbox_id: str, sandbox_domain: str) -> str:
        # return f"{'http' if self.debug else 'https'}://{self.get_host(sandbox_id, sandbox_domain, self.envd_port)}"
        return f"http://{self.get_host(sandbox_id, sandbox_domain, self.envd_port)}"

    from e2b import ConnectionConfig
    ConnectionConfig.get_host = __connection_config_get_host
    ConnectionConfig.get_sandbox_url = __get_sandbox_url

    # patch the API key pattern to compatible old format,
    # new API key format is must start with "e2b_",(e2b version >= 2.25.0)
    from e2b import api
    import re
    api._API_KEY_PATTERN = re.compile(r"\A.{30,}\Z")

