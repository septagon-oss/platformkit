"""The incoming inventory merge preserves both parents' decisions and budget bytes."""
import unittest
from unittest.mock import patch

import inventory_merge_preserves_decisions_test as guard


class IncomingInventoryMergePreservesDecisions(guard.InventoryMergePreservesDecisions):
    def setUp(self):
        merge = patch.object(guard, "MERGE", "84b8b3b5493147a8b6753ee4ef33978a4afff4a3")
        merge.start()
        self.addCleanup(merge.stop)


if __name__ == "__main__":
    unittest.main()
