import sys
sys.path.append("/opt/v/src/consistency-testing/gobekli")
sys.path.append("/opt/v/src/consistency-testing/chaostest")
from chaostest.faults import BaselineRecoverableFault
from rptest.tests.chaos.chaos_base_test import BaseChaosTest
from ducktape.mark.resource import cluster


class IsolateLeaderTest(BaseChaosTest):
    def __init__(self, test_context, num_nodes=3):
        super(IsolateLeaderTest, self).__init__(test_context, num_nodes)

    @cluster(num_nodes=3)
    def test_baseline(self):
        failure_factory = lambda: BaselineRecoverableFault()
        self.run(failure_factory)